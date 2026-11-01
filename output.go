package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
)

// outputInfo describes a finished .m4b for the finish screen.
type outputInfo struct {
	path     string
	size     int64
	duration time.Duration // 0 if ffprobe couldn't read it
}

func describeOutput(ctx context.Context, path string) outputInfo {
	info := outputInfo{path: path}
	if st, err := os.Stat(path); err == nil {
		info.size = st.Size()
	}
	if bb, err := getMetaJsonBytes(ctx, "", path); err == nil {
		var c container
		if json.Unmarshal(bb.Bytes(), &c) == nil {
			if secs, err := strconv.ParseFloat(c.Format.Duration, 64); err == nil {
				info.duration = time.Duration(secs * float64(time.Second))
			}
		}
	}
	return info
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

// humanDuration formats d as h:mm:ss (or m:ss under an hour).
func humanDuration(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func (o outputInfo) summary() string {
	if o.duration > 0 {
		return humanSize(o.size) + " · " + humanDuration(o.duration)
	}
	return humanSize(o.size)
}
