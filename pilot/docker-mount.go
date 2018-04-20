package pilot

import (
	"io/ioutil"
	log "github.com/Sirupsen/logrus"
	"strings"
	"os/exec"
)

const proc_mount_file = "/proc/self/mountinfo"


// mount point 配置
// 主要是 umount 一下
func ConfigDockerMountPoint() error {
	for _, mp := range mountPoints() {
		if err := mp.umount(); err != nil {
			log.Fatalf("can't umount point %s", mp.path, err)
		}
	}

	return nil
}

type mountPoint struct {
	path string
}

func (mp *mountPoint) umount() error {
	cmd := exec.Command("umount", "-l", mp.path)
	return cmd.Run()
}

// 获取mount point 信息
func mountPoints() ([]mountPoint) {
	mps := make([]mountPoint, 0)
	bs, err := ioutil.ReadFile(proc_mount_file)

	if err != nil {
		log.Fatalf("can't read %s , ", proc_mount_file, err)
	}

	txt := string(bs)

	log.Debugf("proc_mount_file = %s", txt)

	for _, lines := range strings.Split(txt, "\n") {
		p := strings.Split(lines, " ")
		if len(p) > 4 {
			mp := p[4]
			if len(lines) > 0 && strings.HasPrefix(mp, "/") && strings.HasSuffix(mp, "shm") && strings.Contains(mp, "containers") {
				mps = append(mps, mountPoint{mp})
			}
		}
	}

	return mps
}
