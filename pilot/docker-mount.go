package pilot

func ConfigDockerMountPoint() error {
	for _, mp := range mountPoints() {
		mp.umount()
	}

	return nil
}

type mountPoint struct {
	path string
}

func (mp *mountPoint) umount() error {
	return nil
}

// 获取mount point 信息
func mountPoints() ([]mountPoint) {
	return make([]mountPoint, 0)
}
