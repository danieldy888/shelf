// Package docx writes minimal Office Open XML word-processing
// documents — a sequence of text paragraphs plus one optional inline
// picture — which Shelf produces when exporting an image together with
// the text recognized in it. It is a small writer, not a docx toolkit,
// and uses only the standard library.
package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Picture is an embedded image. Data holds PNG or JPEG bytes and Ext
// the matching extension ("png", "jpeg" or "jpg"); W and H are its
// pixel size. The pixel size only decides the displayed dimensions —
// the aspect ratio is kept when the picture is scaled to fit the page.
type Picture struct {
	Data []byte
	Ext  string
	W, H int
}

// A4 page with 1-inch margins, in twentieths of a point (twips).
const (
	pageWidth  = 11906
	pageHeight = 16838
	pageMargin = 1440
)

// emuPerPixel converts pixels to EMU at 96 dpi (914400/96 = 9525), the
// natural size a picture is never scaled beyond.
const emuPerPixel = 914400 / 96

// emuPerTwip converts twips to English Metric Units (914400 EMU/inch).
const emuPerTwip = 914400 / 1440 // 635

// Text area in EMU: the page minus 1-inch margins on every side.
const (
	textAreaCX = (pageWidth - 2*pageMargin) * emuPerTwip  // 5731510
	textAreaCY = (pageHeight - 2*pageMargin) * emuPerTwip // 8863330
)

// Write creates a minimal valid .docx at dst: an A4 page with 1-inch
// margins; each string in paragraphs becomes one paragraph (the text is
// kept exactly, including Chinese), then, when pic is non-nil, one
// paragraph holding the picture inline. The picture is shown at its
// natural size (pixels at 96 dpi), scaled down — keeping the aspect
// ratio — when needed to fit the text width and height; it is never
// scaled up. paragraphs may be empty or
// nil, producing a picture-only document. The file is written
// atomically through a temp file in dst's directory plus rename.
func Write(dst string, paragraphs []string, pic *Picture) error {
	if pic != nil {
		if err := pic.validate(); err != nil {
			return err
		}
	}
	data, err := build(paragraphs, pic)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(dst, data, 0o600); err != nil {
		return fmt.Errorf("docx: %w", err)
	}
	return nil
}

// validate rejects pictures the package cannot embed.
func (p *Picture) validate() error {
	if len(p.Data) == 0 {
		return errors.New("docx: picture has no data")
	}
	if p.W <= 0 || p.H <= 0 {
		return fmt.Errorf("docx: picture size %dx%d is invalid", p.W, p.H)
	}
	if p.mimeType() == "" {
		return fmt.Errorf("docx: unsupported picture extension %q", p.Ext)
	}
	return nil
}

// mimeType maps the file extension to an OOXML image content type.
func (p *Picture) mimeType() string {
	switch strings.ToLower(p.Ext) {
	case "png":
		return "image/png"
	case "jpeg", "jpg":
		return "image/jpeg"
	}
	return ""
}

// fileExt is the lowercased extension used for the media file name.
func (p *Picture) fileExt() string {
	return strings.ToLower(p.Ext)
}

// pictureExtent returns the inline extent in EMU: the picture's natural
// size (pixels at 96 dpi) scaled down — keeping the aspect ratio — to
// fit the text width and height, never scaled up beyond natural.
func pictureExtent(p *Picture) (cx, cy int64) {
	cx = int64(p.W) * emuPerPixel
	cy = int64(p.H) * emuPerPixel
	if cx > textAreaCX {
		cy = int64(p.H) * textAreaCX / int64(p.W)
		cx = textAreaCX
	}
	if cy > textAreaCY {
		cx = int64(p.W) * textAreaCY / int64(p.H)
		cy = textAreaCY
	}
	return cx, cy
}

// build assembles the whole package in memory and returns the .docx
// bytes. Media is a few megabytes at most, so buffering is fine.
func build(paragraphs []string, pic *Picture) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, xml.Header+body)
		return err
	}
	addRaw := func(name string, data []byte) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}

	var mediaName string
	if pic != nil {
		mediaName = "image1." + pic.fileExt()
	}
	if err := add("[Content_Types].xml", contentTypesXML(pic)); err != nil {
		return nil, err
	}
	if err := add("_rels/.rels", rootRelsXML); err != nil {
		return nil, err
	}
	if err := add("docProps/core.xml", corePropsXML(time.Now().UTC())); err != nil {
		return nil, err
	}
	if err := add("docProps/app.xml", appPropsXML); err != nil {
		return nil, err
	}
	if err := add("word/document.xml", documentXML(paragraphs, pic, mediaName)); err != nil {
		return nil, err
	}
	if pic != nil {
		if err := add("word/_rels/document.xml.rels", documentRelsXML(mediaName)); err != nil {
			return nil, err
		}
		if err := addRaw("word/media/"+mediaName, pic.Data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// contentTypesXML lists the defaults and overrides for every part. The
// picture extension gets a Default entry so image1.<ext> resolves.
func contentTypesXML(pic *Picture) string {
	const open = `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>`
	const overrides = `<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
		`<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>` +
		`<Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/>` +
		`</Types>`
	if pic == nil {
		return open + overrides
	}
	return open +
		fmt.Sprintf(`<Default Extension="%s" ContentType="%s"/>`, pic.fileExt(), pic.mimeType()) +
		overrides
}

// rootRelsXML points the package at the document part and the doc
// property parts.
const rootRelsXML = `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
	`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>` +
	`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/>` +
	`</Relationships>`

// documentRelsXML binds rId1, referenced by the drawing's a:blip, to
// the media file.
func documentRelsXML(mediaName string) string {
	return `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		fmt.Sprintf(`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/%s"/>`, mediaName) +
		`</Relationships>`
}

// corePropsXML stamps the file as produced by Shelf.
func corePropsXML(now time.Time) string {
	ts := now.Format("2006-01-02T15:04:05Z")
	return `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` +
		`<dc:creator>Shelf</dc:creator><cp:lastModifiedBy>Shelf</cp:lastModifiedBy>` +
		fmt.Sprintf(`<dcterms:created xsi:type="dcterms:W3CDTF">%s</dcterms:created>`, ts) +
		fmt.Sprintf(`<dcterms:modified xsi:type="dcterms:W3CDTF">%s</dcterms:modified>`, ts) +
		`</cp:coreProperties>`
}

const appPropsXML = `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes">` +
	`<Application>Shelf</Application>` +
	`</Properties>`

// documentXML renders the body: one w:p per paragraph, then the picture
// paragraph, then the section properties fixing the A4 layout.
func documentXML(paragraphs []string, pic *Picture, mediaName string) string {
	var b strings.Builder
	b.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><w:body>`)
	for _, p := range paragraphs {
		if p == "" {
			b.WriteString(`<w:p/>`)
			continue
		}
		// The East Asian font hint keeps Chinese readable in Word.
		b.WriteString(`<w:p><w:r><w:rPr><w:rFonts w:eastAsia="PingFang SC"/></w:rPr><w:t xml:space="preserve">`)
		escapeText(&b, p)
		b.WriteString(`</w:t></w:r></w:p>`)
	}
	if pic != nil {
		cx, cy := pictureExtent(pic)
		fmt.Fprintf(&b, `<w:p><w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0">`+
			`<wp:extent cx="%d" cy="%d"/><wp:effectExtent l="0" t="0" r="0" b="0"/>`+
			`<wp:docPr id="1" name="%s" descr="%s"/><wp:cNvGraphicFramePr/>`+
			`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic>`+
			`<pic:nvPicPr><pic:cNvPr id="0" name="%s"/><pic:cNvPicPr/></pic:nvPicPr>`+
			`<pic:blipFill><a:blip r:embed="rId1"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>`+
			`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm>`+
			`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr>`+
			`</pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`,
			cx, cy, mediaName, mediaName, mediaName, cx, cy)
	}
	fmt.Fprintf(&b, `<w:sectPr><w:pgSz w:w="%d" w:h="%d"/>`+
		`<w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d" w:header="720" w:footer="720" w:gutter="0"/>`+
		`</w:sectPr></w:body></w:document>`,
		pageWidth, pageHeight, pageMargin, pageMargin, pageMargin, pageMargin)
	return b.String()
}

// escapeText writes s to b with XML character escaping.
func escapeText(b *strings.Builder, s string) {
	if err := xml.EscapeText(b, []byte(s)); err != nil {
		// strings.Builder never fails; the write is unconditional.
		b.WriteString(s)
	}
}

// writeFileAtomic writes data to dst through a temp file in the same
// directory plus rename, so a crash never leaves a half-written .docx.
func writeFileAtomic(dst string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(dst)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, dst)
}
