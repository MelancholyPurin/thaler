// Package units formats quantities for people to read.
package units

import "fmt"

// HumanSize is a file size for reading: 512 B, 12.4 KB, 3.1 MB.
func HumanSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}
