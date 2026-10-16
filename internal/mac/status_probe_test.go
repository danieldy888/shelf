//go:build darwin

package mac

import (
	"os"
	"testing"
	"time"
)

// SHELF_HW_PROBE=1 runs read-only hardware probes that print the current
// power and output state. The test changes nothing: IOPowerSources and the
// HAL are only ever read, listeners can only raise a flag, and no power or
// volume setter exists in this package to call.
//
// Note: the HAL may deny property reads to `go test`'s process context on
// some systems (the same reads succeed in a plain binary or a native
// probe); ReadOutput then reports ok=false, which the volume module treats
// as "no controllable device".
func TestHardwareProbe(t *testing.T) {
	if os.Getenv("SHELF_HW_PROBE") != "1" {
		t.Skip("set SHELF_HW_PROBE=1 to run the read-only hardware probe")
	}
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	p := ReadPower()
	t.Logf("power: hasBattery=%v source=%q percent=%d charging=%v",
		p.HasBattery, providingPowerSource(), p.Percent, p.Charging)
	t.Logf("power watch installed: %v (err: %v)", WatchPowerSources(), powerErr)
	out := ReadOutput()
	t.Logf("output: ok=%v level=%.3f muted=%v (err: %v)", out.OK, out.Level, out.Muted, audioErr)
	t.Logf("output watch installed: %v", WatchOutput())
	// A moment later: a stale event flag here would make the app's first
	// tick sample twice at most, which is harmless.
	time.Sleep(50 * time.Millisecond)
	t.Logf("pending: power=%v output=%v", powerDirty.Load(), audioDirty.Load())
}
