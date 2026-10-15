//go:build darwin

package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"golang.org/x/image/draw"

	"shelf/internal/mac"
	"shelf/internal/store"
)

// maxConcurrentThumbs 限制同时在后台解码的缩略图数，避免多张大图同时解码。
const maxConcurrentThumbs = 2

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
	// 并发解码上限 2：名额满则留待完成回调触发的重绘再试，
	// 不排队、不另起后台 goroutine。
	if a.thumbActive >= maxConcurrentThumbs {
		return nil
	}
	a.thumbActive++
	a.thumbPending[item.ID] = true
	go func() {
		var b *ui.Bitmap
		ext := strings.ToLower(filepath.Ext(item.Path))
		if item.Kind == store.KindImage || ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" || ext == ".webp" || ext == ".bmp" {
			if img := decodeThumb(item.Path); img != nil {
				b = ui.NewBitmap(img)
			}
		}
		a.win.Update(func() {
			a.thumbActive--
			if a.quitting {
				return
			}
			delete(a.thumbPending, item.ID)
			// invalidateThumbnail 标记的在途结果直接丢弃，由重绘按上限重新解码。
			if a.thumbDiscard[item.ID] {
				delete(a.thumbDiscard, item.ID)
				return
			}
			// 已删除的项不取 Finder 图标、不把缓存重新放回来。
			if a.store != nil {
				for _, current := range a.store.Items {
					if current.ID == item.ID {
						if b == nil {
							if icon := mac.FileIcon(item.Path, 40); len(icon) > 0 {
								b, _ = ui.DecodeBitmap(icon)
							}
						}
						a.thumbnails[item.ID] = b
						break
					}
				}
			}
		})
	}()
	return nil
}

// invalidateThumbnail 丢弃缓存的缩略图；若解码仍在进行，标记其结果
// 在完成回调中丢弃而不写回缓存，由随后的重绘按并发上限重新解码。
func (a *shelf) invalidateThumbnail(id string) {
	delete(a.thumbnails, id)
	if a.thumbPending[id] {
		if a.thumbDiscard == nil {
			a.thumbDiscard = make(map[string]bool)
		}
		a.thumbDiscard[id] = true
	}
}

// clearThumbnails 丢弃全部缓存缩略图；仍在解码的在途结果标记为完成回调
// 中丢弃而不写回缓存。清空后缩略图不会再被旧解码结果填回：完成回调里
// thumbDiscard 命中直接丢弃，未命中的也要求该项仍是 store 成员才写回。
// 同一项在旧解码在途时重新加入，thumbDiscard 也会丢弃过期结果，由随后的
// 重绘按并发上限重新解码。
func (a *shelf) clearThumbnails() {
	clear(a.thumbnails)
	for id := range a.thumbPending {
		if a.thumbDiscard == nil {
			a.thumbDiscard = make(map[string]bool)
		}
		a.thumbDiscard[id] = true
	}
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
