//go:build darwin

package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"golang.org/x/image/draw"

	"shelf/internal/mac"
	"shelf/internal/store"
)

func (a *shelf) thumbnail(item store.Item) *ui.Bitmap {
	if item.Kind == store.KindText || item.Missing {
		return nil
	}
	if b, ok := a.thumbnails[item.ID]; ok {
		return b
	}
	if a.thumbPending[item.ID] {
		return nil
	}
	a.thumbPending[item.ID] = true
	go func() {
		var b *ui.Bitmap
		ext := strings.ToLower(filepath.Ext(item.Path))
		if item.Kind == store.KindImage || ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" || ext == ".webp" || ext == ".bmp" {
			// 一次读入，尺寸检查通过后才解码；后台降采样，不把原图常驻 GPU。
			if f, err := os.Open(item.Path); err == nil {
				cfg, _, err := image.DecodeConfig(f)
				if err == nil && cfg.Width > 0 && cfg.Height > 0 && int64(cfg.Width)*int64(cfg.Height) <= 40_000_000 {
					if _, err = f.Seek(0, 0); err == nil {
						if img, _, err := image.Decode(f); err == nil {
							w, h := cfg.Width, cfg.Height
							if w >= h {
								h = max(1, h*80/w)
								w = 80
							} else {
								w = max(1, w*80/h)
								h = 80
							}
							thumb := image.NewNRGBA(image.Rect(0, 0, w, h))
							draw.ApproxBiLinear.Scale(thumb, thumb.Bounds(), img, img.Bounds(), draw.Src, nil)
							b = ui.NewBitmap(thumb)
						}
					}
				}
				f.Close()
			}
		}
		a.win.Update(func() {
			if a.quitting {
				return
			}
			if b == nil {
				if icon := mac.FileIcon(item.Path, 40); len(icon) > 0 {
					b, _ = ui.DecodeBitmap(icon)
				}
			}
			delete(a.thumbPending, item.ID)
			// 已删除的项不把缓存重新放回来。
			if a.store != nil {
				for _, current := range a.store.Items {
					if current.ID == item.ID {
						a.thumbnails[item.ID] = b
						break
					}
				}
			}
		})
	}()
	return nil
}

func trayIcon() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	ink := image.NewUniform(color.NRGBA{A: 255})
	for _, r := range []image.Rectangle{
		image.Rect(5, 6, 7, 26), image.Rect(25, 6, 27, 26), image.Rect(5, 24, 27, 26),
		image.Rect(7, 17, 12, 19), image.Rect(20, 17, 25, 19), image.Rect(12, 19, 20, 21),
		image.Rect(15, 4, 17, 15), image.Rect(11, 11, 21, 13),
	} {
		draw.Draw(img, r, ink, image.Point{}, draw.Src)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil
	}
	return b.Bytes()
}
