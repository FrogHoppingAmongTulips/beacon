//go:build !linux

package metrics

// readDisk на не-Linux системах (напр. macOS при разработке) возвращает нули без паники.
func readDisk(path string) (total, free uint64) { return 0, 0 }
