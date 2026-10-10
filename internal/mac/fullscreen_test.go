//go:build darwin

package mac

import (
	"testing"
)

// AppKit 规定 FullScreenPrimary / FullScreenAuxiliary /
// FullScreenNone 三个位最多只能设置一个。mygo 的 DisableFullScreen 会设
// None，旧代码直接 OR 上 Auxiliary 留下两个互斥位。
//
// 真实窗口上的集成证据来自运行中的应用：main 把 JoinFullScreenSpaces
// 返回的 AppKit 读回值写入日志（window collectionBehavior=0x…），并在
// 全屏 Space 中做快捷键唤出/边缘拖入/收起的实测。AppKit 的 NSWindow 仅
// 允许在主线程创建，而 go test 不保证测试 goroutine 位于主线程（重复
// 运行会抛 NSInternalInconsistencyException），因此这里只做确定性的
// 位运算测试，矩阵覆盖互斥位与无关位的组合。
func TestResolveCollectionBehaviorClearsConflictingFullScreenBits(t *testing.T) {
	const fsMask = collectionFullScreenPrimary | collectionFullScreenAuxiliary | collectionFullScreenNone
	for _, current := range []uint{
		0,
		collectionFullScreenPrimary,
		collectionFullScreenNone,
		collectionFullScreenPrimary | collectionFullScreenNone,
		collectionFullScreenNone | collectionFullScreenAuxiliary | (1 << 4),
		collectionFullScreenPrimary | collectionCanJoinAllSpaces,
	} {
		got := resolveCollectionBehavior(current)
		if bits := got & fsMask; bits != collectionFullScreenAuxiliary {
			t.Fatalf("resolveCollectionBehavior(%#x) leaves fullscreen bits %#x, want only auxiliary", current, bits)
		}
		if got&collectionCanJoinAllSpaces == 0 {
			t.Fatalf("resolveCollectionBehavior(%#x) drops CanJoinAllSpaces", current)
		}
		// 与全屏无关的位必须原样保留。
		if preserved := current &^ (fsMask | collectionCanJoinAllSpaces); got&preserved != preserved {
			t.Fatalf("resolveCollectionBehavior(%#x) lost unrelated bits: %#x", current, preserved)
		}
	}
}
