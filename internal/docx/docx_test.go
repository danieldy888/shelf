package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// pngBytes renders a w×h PNG the way the converter hands pictures to
// Write.
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// unzip reads a .docx into a name → bytes map.
func unzip(t *testing.T, path string) map[string][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(f, st.Size())
	if err != nil {
		t.Fatalf("%s is not a zip: %v", path, err)
	}
	parts := map[string][]byte{}
	for _, zf := range zr.File {
		r, err := zf.Open()
		if err != nil {
			t.Fatalf("open part %s: %v", zf.Name, err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatalf("read part %s: %v", zf.Name, err)
		}
		parts[zf.Name] = data
	}
	return parts
}

// assertWellFormed parses every part that claims to be XML.
func assertWellFormed(t *testing.T, parts map[string][]byte) {
	t.Helper()
	for name, data := range parts {
		if !strings.HasSuffix(name, ".xml") && !strings.HasSuffix(name, ".rels") {
			continue
		}
		dec := xml.NewDecoder(bytes.NewReader(data))
		for {
			if _, err := dec.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("part %s is not well-formed XML: %v\n%s", name, err, data)
			}
		}
	}
}

// paraInfo is one w:p from document.xml: its text plus whether it
// holds a <w:br w:type="page"/> break.
type paraInfo struct {
	text      string
	pageBreak bool
}

// paragraphTexts returns the text of every w:p in document.xml, in
// order.
func paragraphTexts(t *testing.T, documentXML []byte) []string {
	t.Helper()
	infos := paragraphInfos(t, documentXML)
	texts := make([]string, len(infos))
	for i, p := range infos {
		texts[i] = p.text
	}
	return texts
}

// paragraphInfos parses document.xml into per-paragraph records, in
// order; runs inside a paragraph are concatenated and a
// <w:br w:type="page"/> inside a run sets pageBreak.
func paragraphInfos(t *testing.T, documentXML []byte) []paraInfo {
	t.Helper()
	const wNS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	dec := xml.NewDecoder(bytes.NewReader(documentXML))
	var infos []paraInfo
	inP, inT := false, false
	var cur paraInfo
	var text strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("decode document.xml: %v", err)
		}
		switch e := tok.(type) {
		case xml.StartElement:
			switch {
			case e.Name.Space == wNS && e.Name.Local == "p":
				inP = true
				cur = paraInfo{}
				text.Reset()
			case inP && e.Name.Space == wNS && e.Name.Local == "br":
				for _, a := range e.Attr {
					if a.Name.Local == "type" && a.Value == "page" {
						cur.pageBreak = true
					}
				}
			case e.Name.Space == wNS && e.Name.Local == "t":
				inT = true
			}
		case xml.EndElement:
			switch {
			case e.Name.Space == wNS && e.Name.Local == "p":
				cur.text = text.String()
				infos = append(infos, cur)
				inP = false
			case e.Name.Space == wNS && e.Name.Local == "t":
				inT = false
			}
		case xml.CharData:
			if inP && inT {
				text.Write(e)
			}
		}
	}
	return infos
}

// inlineExtent returns the wp:inline extent (EMU) of the first drawing.
func inlineExtent(t *testing.T, documentXML []byte) (cx, cy int64) {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(documentXML))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("decode document.xml: %v", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "extent" ||
			se.Name.Space != "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" {
			continue
		}
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "cx":
				cx, _ = strconv.ParseInt(a.Value, 10, 64)
			case "cy":
				cy, _ = strconv.ParseInt(a.Value, 10, 64)
			}
		}
		return cx, cy
	}
	t.Fatal("document.xml has no wp:extent")
	return 0, 0
}

// mediaForRID resolves a relationship id in word/_rels/document.xml.rels
// to the media file bytes it targets.
func mediaForRID(t *testing.T, parts map[string][]byte, rid string) []byte {
	t.Helper()
	rels, ok := parts["word/_rels/document.xml.rels"]
	if !ok {
		t.Fatal("word/_rels/document.xml.rels missing")
	}
	type rel struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
	}
	type relsDoc struct {
		Rels []rel `xml:"Relationship"`
	}
	var doc relsDoc
	if err := xml.Unmarshal(rels, &doc); err != nil {
		t.Fatalf("parse document.xml.rels: %v", err)
	}
	for _, r := range doc.Rels {
		if r.ID == rid {
			data, ok := parts["word/"+r.Target]
			if !ok {
				t.Fatalf("relationship %s targets %q, not in package", rid, r.Target)
			}
			return data
		}
	}
	t.Fatalf("relationship %s not found in document.xml.rels", rid)
	return nil
}

// embedID finds the r:embed attribute the drawing uses.
func embedID(t *testing.T, documentXML []byte) string {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(documentXML))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("decode document.xml: %v", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "blip" {
			continue
		}
		for _, a := range se.Attr {
			if a.Name.Local == "embed" {
				return a.Value
			}
		}
	}
	t.Fatal("document.xml has no a:blip r:embed")
	return ""
}

// assertAspect checks the extent keeps pic's aspect ratio.
func assertAspect(t *testing.T, pic *Picture, cx, cy int64) {
	t.Helper()
	got := float64(cx) / float64(cy)
	want := float64(pic.W) / float64(pic.H)
	if d := got/want - 1; d < -0.001 || d > 0.001 {
		t.Fatalf("extent %dx%d loses aspect ratio: got %.4f want %.4f", cx, cy, got, want)
	}
}

// assertFits checks the extent stays inside the A4 text area.
func assertFits(t *testing.T, cx, cy int64) {
	t.Helper()
	if cx <= 0 || cy <= 0 {
		t.Fatalf("extent %dx%d must be positive", cx, cy)
	}
	if cx > textAreaCX || cy > textAreaCY {
		t.Fatalf("extent %dx%d exceeds text area %dx%d", cx, cy, textAreaCX, textAreaCY)
	}
}

func docPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "out.docx")
}

func TestWriteTextParagraphs(t *testing.T) {
	dst := docPath(t)
	paragraphs := []string{
		"识别结果第一行",
		"a<b & c> d\"e'f",
		"trailing space kept ",
		"",
		"last line",
	}
	if err := Write(dst, paragraphs, nil); err != nil {
		t.Fatalf("Write: %v", err)
	}
	parts := unzip(t, dst)
	assertWellFormed(t, parts)
	got := paragraphTexts(t, parts["word/document.xml"])
	if len(got) != len(paragraphs) {
		t.Fatalf("document has %d paragraphs, want %d", len(got), len(paragraphs))
	}
	for i, want := range paragraphs {
		if got[i] != want {
			t.Errorf("paragraph %d = %q, want %q", i, got[i], want)
		}
	}
	for name := range parts {
		if strings.HasPrefix(name, "word/media/") {
			t.Errorf("text-only document contains media part %s", name)
		}
	}
	if _, ok := parts["word/_rels/document.xml.rels"]; ok {
		t.Error("text-only document should not need document.xml.rels")
	}
}

func TestWriteWidePicture(t *testing.T) {
	dst := docPath(t)
	pic := &Picture{Data: pngBytes(t, 4000, 1000), Ext: "png", W: 4000, H: 1000}
	if err := Write(dst, []string{"标题"}, pic); err != nil {
		t.Fatalf("Write: %v", err)
	}
	parts := unzip(t, dst)
	assertWellFormed(t, parts)
	cx, cy := inlineExtent(t, parts["word/document.xml"])
	// Wide picture: fills the text width.
	if cx != textAreaCX {
		t.Fatalf("wide picture cx = %d, want text width %d", cx, textAreaCX)
	}
	assertFits(t, cx, cy)
	assertAspect(t, pic, cx, cy)
	if got := mediaForRID(t, parts, embedID(t, parts["word/document.xml"])); !bytes.Equal(got, pic.Data) {
		t.Fatal("embedded media bytes differ from the input picture")
	}
	if _, ok := parts["word/media/image1.png"]; !ok {
		t.Error("word/media/image1.png missing")
	}
}

func TestWriteTallPicture(t *testing.T) {
	dst := docPath(t)
	pic := &Picture{Data: pngBytes(t, 1000, 4000), Ext: "png", W: 1000, H: 4000}
	if err := Write(dst, nil, pic); err != nil {
		t.Fatalf("Write: %v", err)
	}
	parts := unzip(t, dst)
	assertWellFormed(t, parts)
	cx, cy := inlineExtent(t, parts["word/document.xml"])
	// Tall picture: capped by the text height instead of the width.
	if cy != textAreaCY {
		t.Fatalf("tall picture cy = %d, want text height %d", cy, textAreaCY)
	}
	assertFits(t, cx, cy)
	assertAspect(t, pic, cx, cy)
	if got := mediaForRID(t, parts, embedID(t, parts["word/document.xml"])); !bytes.Equal(got, pic.Data) {
		t.Fatal("embedded media bytes differ from the input picture")
	}
}

func TestWriteJPEG(t *testing.T) {
	dst := docPath(t)
	// Minimal JPEG header bytes suffice: Write stores Data verbatim.
	jpegData := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0xFF, 0xD9}
	pic := &Picture{Data: jpegData, Ext: "jpeg", W: 16, H: 9}
	if err := Write(dst, nil, pic); err != nil {
		t.Fatalf("Write: %v", err)
	}
	parts := unzip(t, dst)
	assertWellFormed(t, parts)
	if !bytes.Equal(parts["word/media/image1.jpeg"], jpegData) {
		t.Fatal("jpeg bytes not stored verbatim")
	}
	ct := string(parts["[Content_Types].xml"])
	if !strings.Contains(ct, `Extension="jpeg" ContentType="image/jpeg"`) {
		t.Error("content types do not declare image/jpeg")
	}
}
func TestWriteSmallPicture(t *testing.T) {
	dst := docPath(t)
	pic := &Picture{Data: pngBytes(t, 320, 200), Ext: "png", W: 320, H: 200}
	if err := Write(dst, nil, pic); err != nil {
		t.Fatalf("Write: %v", err)
	}
	parts := unzip(t, dst)
	assertWellFormed(t, parts)
	cx, cy := inlineExtent(t, parts["word/document.xml"])
	// A picture smaller than the text area keeps its natural size at
	// 96 dpi: 1 px = 9525 EMU, never scaled up.
	if wantX, wantY := int64(320*9525), int64(200*9525); cx != wantX || cy != wantY {
		t.Fatalf("small picture extent = %dx%d, want natural %dx%d", cx, cy, wantX, wantY)
	}
	assertFits(t, cx, cy)
	assertAspect(t, pic, cx, cy)
}

func TestWritePictureOnlyDocument(t *testing.T) {
	dst := docPath(t)
	pic := &Picture{Data: pngBytes(t, 640, 480), Ext: "png", W: 640, H: 480}
	if err := Write(dst, nil, pic); err != nil {
		t.Fatalf("Write: %v", err)
	}
	parts := unzip(t, dst)
	assertWellFormed(t, parts)
	if got := paragraphTexts(t, parts["word/document.xml"]); len(got) != 1 || got[0] != "" {
		t.Fatalf("picture-only document paragraphs = %v", got)
	}
	cx, cy := inlineExtent(t, parts["word/document.xml"])
	assertFits(t, cx, cy)
	assertAspect(t, pic, cx, cy)
}

func TestWriteValidation(t *testing.T) {
	for name, pic := range map[string]*Picture{
		"empty data": {Ext: "png", W: 10, H: 10},
		"zero size":  {Data: []byte{1}, Ext: "png"},
		"bad ext":    {Data: []byte{1}, Ext: "gif", W: 10, H: 10},
	} {
		dst := filepath.Join(t.TempDir(), "out.docx")
		if err := Write(dst, nil, pic); err == nil {
			t.Errorf("%s: Write succeeded, want error", name)
		} else if !strings.HasPrefix(err.Error(), "docx:") {
			t.Errorf("%s: error %q lacks docx: prefix", name, err)
		}
		if _, err := os.Stat(dst); !os.IsNotExist(err) {
			t.Errorf("%s: dst was created on failure", name)
		}
	}
}

func TestWriteAtomicNoPartialFile(t *testing.T) {
	// A destination in a missing directory fails without leaving files.
	dst := filepath.Join(t.TempDir(), "missing", "out.docx")
	if err := Write(dst, []string{"x"}, nil); err == nil {
		t.Fatal("Write to missing directory succeeded")
	}
}

// TestWriteTextutilRoundTrip feeds the document through macOS textutil
// and expects every paragraph back.
func TestWriteTextutilRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("textutil"); err != nil {
		t.Skip("textutil not available")
	}
	dst := docPath(t)
	paragraphs := []string{"识别结果第一行", "Hello docx 世界", "Last <line> & done"}
	pic := &Picture{Data: pngBytes(t, 320, 200), Ext: "png", W: 320, H: 200}
	if err := Write(dst, paragraphs, pic); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out, err := exec.Command("textutil", "-convert", "txt", "-stdout", dst).Output()
	if err != nil {
		t.Fatalf("textutil: %v", err)
	}
	txt := string(out)
	for _, want := range paragraphs {
		if !strings.Contains(txt, want) {
			t.Errorf("textutil output lacks %q\nfull output:\n%s", want, txt)
		}
	}
}

// TestWriteDeterministicStructure pins down the required part list.
func TestWriteDeterministicStructure(t *testing.T) {
	dst := docPath(t)
	pic := &Picture{Data: pngBytes(t, 10, 10), Ext: "png", W: 10, H: 10}
	if err := Write(dst, []string{"a"}, pic); err != nil {
		t.Fatalf("Write: %v", err)
	}
	parts := unzip(t, dst)
	for _, name := range []string{
		"[Content_Types].xml",
		"_rels/.rels",
		"word/document.xml",
		"word/_rels/document.xml.rels",
		"word/media/image1.png",
	} {
		if _, ok := parts[name]; !ok {
			t.Errorf("required part %s missing (have %v)", name, keys(parts))
		}
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// --- WritePages -----------------------------------------------------

func TestWritePagesBreakPositions(t *testing.T) {
	dst := docPath(t)
	pages := [][]string{
		{"p1a", "p1b"},
		{"p2a"},
		{"p3a", "p3b", "p3c"},
	}
	if err := WritePages(dst, pages); err != nil {
		t.Fatalf("WritePages: %v", err)
	}
	parts := unzip(t, dst)
	assertWellFormed(t, parts)
	infos := paragraphInfos(t, parts["word/document.xml"])
	// Pages are separated by dedicated break paragraphs in document
	// order: p1a, p1b, [break], p2a, [break], p3a, p3b, p3c.
	want := []paraInfo{
		{text: "p1a"},
		{text: "p1b"},
		{pageBreak: true},
		{text: "p2a"},
		{pageBreak: true},
		{text: "p3a"},
		{text: "p3b"},
		{text: "p3c"},
	}
	if len(infos) != len(want) {
		t.Fatalf("paragraph count = %d, want %d: %+v", len(infos), len(want), infos)
	}
	breaks := 0
	for i, p := range infos {
		if p != want[i] {
			t.Errorf("paragraph %d = %+v, want %+v", i, p, want[i])
		}
		if p.pageBreak {
			breaks++
		}
	}
	if breaks != 2 {
		t.Fatalf("%d page breaks for 3 pages, want 2", breaks)
	}
	for name := range parts {
		if strings.HasPrefix(name, "word/media/") {
			t.Errorf("text-only document contains media part %s", name)
		}
	}
	if _, ok := parts["word/_rels/document.xml.rels"]; ok {
		t.Error("WritePages document should not need document.xml.rels")
	}
}

func TestWritePagesEmptyMiddlePage(t *testing.T) {
	dst := docPath(t)
	pages := [][]string{
		{"first"},
		{}, // empty page: still produces its page
		{"last"},
	}
	if err := WritePages(dst, pages); err != nil {
		t.Fatalf("WritePages: %v", err)
	}
	parts := unzip(t, dst)
	assertWellFormed(t, parts)
	infos := paragraphInfos(t, parts["word/document.xml"])
	// first, [break], (empty page 2 paragraph), [break], last
	want := []paraInfo{
		{text: "first"},
		{pageBreak: true},
		{text: ""},
		{pageBreak: true},
		{text: "last"},
	}
	if len(infos) != len(want) {
		t.Fatalf("paragraphs = %+v, want %+v", infos, want)
	}
	for i, p := range infos {
		if p != want[i] {
			t.Errorf("paragraph %d = %+v, want %+v", i, p, want[i])
		}
	}
}

func TestWritePagesZeroPages(t *testing.T) {
	for _, pages := range [][][]string{nil, {}} {
		dst := docPath(t)
		if err := WritePages(dst, pages); err != nil {
			t.Fatalf("WritePages(%v): %v", pages, err)
		}
		parts := unzip(t, dst)
		assertWellFormed(t, parts)
		infos := paragraphInfos(t, parts["word/document.xml"])
		if len(infos) != 1 || infos[0].text != "" || infos[0].pageBreak {
			t.Fatalf("zero-page document paragraphs = %+v, want one empty paragraph", infos)
		}
	}
}

func TestWritePagesSinglePageNoBreak(t *testing.T) {
	dst := docPath(t)
	if err := WritePages(dst, [][]string{{"only", "page"}}); err != nil {
		t.Fatalf("WritePages: %v", err)
	}
	parts := unzip(t, dst)
	for _, p := range paragraphInfos(t, parts["word/document.xml"]) {
		if p.pageBreak {
			t.Errorf("single page paragraph %q has a page break", p.text)
		}
	}
}

func TestWritePagesTextutilRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("textutil"); err != nil {
		t.Skip("textutil not available")
	}
	dst := docPath(t)
	pages := [][]string{
		{"第一页开头", "page one end"},
		{},
		{"第三页", "final line"},
	}
	var flat []string
	for _, page := range pages {
		flat = append(flat, page...)
	}
	if err := WritePages(dst, pages); err != nil {
		t.Fatalf("WritePages: %v", err)
	}
	out, err := exec.Command("textutil", "-convert", "txt", "-stdout", dst).Output()
	if err != nil {
		t.Fatalf("textutil: %v", err)
	}
	txt := string(out)
	pos := 0
	for _, want := range flat {
		i := strings.Index(txt[pos:], want)
		if i < 0 {
			t.Fatalf("textutil output lacks %q after offset %d\nfull output:\n%s", want, pos, txt)
		}
		pos += i + len(want)
	}
}
