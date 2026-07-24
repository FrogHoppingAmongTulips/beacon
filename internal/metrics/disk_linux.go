package metrics

import "syscall"

// readDisk возвращает total/free байт на файловой системе, где смонтирован path.
func readDisk(path string) (total, free uint64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	bsize := uint64(st.Bsize)
	return st.Blocks * bsize, st.Bavail * bsize
}
