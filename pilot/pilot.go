package pilot

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"golang.org/x/net/context"
	"github.com/davecgh/go-spew/spew"
)

/**
Label:
aliyun.log: /var/log/hello.log[:json][;/var/log/abc/def.log[:txt]]
*/

const ENV_PILOT_LOG_PREFIX = "PILOT_LOG_PREFIX"
const ENV_PILOT_TYPE = "PILOT_TYPE"
const ENV_PILOT_CREATE_SYMLINK = "PILOT_CREATE_SYMLINK"
const ENV_FLUENTD_OUTPUT = "FLUENTD_OUTPUT"
const ENV_FILEBEAT_OUTPUT = "FILEBEAT_OUTPUT"

const LABEL_SERVICE_LOGS_TEMPL = "%s.logs."
const ENV_SERVICE_LOGS_TEMPL = "%s_logs_"
const SYMLINK_LOGS_BASE = "/acs/log/"

const LABEL_PROJECT = "com.docker.compose.project"
const LABEL_PROJECT_SWARM_MODE = "com.docker.stack.namespace"
const LABEL_SERVICE = "com.docker.compose.service"
const LABEL_SERVICE_SWARM_MODE = "com.docker.swarm.service.name"
const LABEL_POD = "io.kubernetes.pod.name"

const LABEL_K8S_POD_NAMESPACE = "io.kubernetes.pod.namespace"
const LABEL_K8S_CONTAINER_NAME = "io.kubernetes.container.name"

const LABEL_RANCHER_STACK = "io.rancher.stack.name"
const LABEL_RANCHER_STACK_SERVICE = "io.rancher.stack_service.name"

var NODE_NAME = os.Getenv("NODE_NAME")

const ERR_ALREADY_STARTED = "already started"

type Pilot struct {
	mutex         sync.Mutex
	tpl           *template.Template
	base          string
	dockerClient  *client.Client
	reloadChan    chan bool
	lastReload    time.Time
	piloter       Piloter
	logPrefix     []string
	createSymlink bool
}

type Piloter interface {
	Name() string
	Start() error
	Reload() error
	Stop() error
	ConfHome() string
	ConfPathOf(container string) string
	OnDestroyEvent(container string) error
}

//
func Run(tpl string, baseDir string) error {
	p, err := New(tpl, baseDir)
	if err != nil {
		panic(err)
	}
	return p.watch()
}

func New(tplStr string, baseDir string) (*Pilot, error) {
	tpl, err := template.New("pilot").Parse(tplStr)
	if err != nil {
		return nil, err
	}

	if os.Getenv("DOCKER_API_VERSION") == "" {
		os.Setenv("DOCKER_API_VERSION", "1.23")
	}

	client, err := client.NewEnvClient()
	if err != nil {
		return nil, err
	}

	piloter, _ := NewFilebeatPiloter(baseDir)

	if os.Getenv(ENV_PILOT_TYPE) == PILOT_FLUENTD {
		piloter, _ = NewFluentdPiloter()
	}

	logPrefix := []string{"aliyun"}
	if os.Getenv(ENV_PILOT_LOG_PREFIX) != "" {
		envLogPrefix := os.Getenv(ENV_PILOT_LOG_PREFIX)
		logPrefix = strings.Split(envLogPrefix, ",")
	}

	createSymlink := os.Getenv(ENV_PILOT_CREATE_SYMLINK) == "true"
	return &Pilot{
		dockerClient:  client,
		tpl:           tpl,
		base:          baseDir,
		reloadChan:    make(chan bool),
		piloter:       piloter,
		logPrefix:     logPrefix,
		createSymlink: createSymlink,
	}, nil
}

func (p *Pilot) watch() error {
	if err := p.processAllContainers(); err != nil {
		return err
	}

	err := p.piloter.Start()
	if err != nil && ERR_ALREADY_STARTED != err.Error() {
		return err
	}

	p.lastReload = time.Now()
	go p.doReload()

	ctx := context.Background()
	filter := filters.NewArgs()
	filter.Add("type", "container")

	options := types.EventsOptions{
		Filters: filter,
	}
	msgs, errs := p.client().Events(ctx, options)
	for {
		select {
		case msg := <-msgs:
			if err := p.processEvent(msg); err != nil {
				log.Errorf("fail to process event: %v,  %v", msg, err)
			}
		case err := <-errs:
			log.Warnf("error: %v", err)
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			} else {
				msgs, errs = p.client().Events(ctx, options)
			}
		}
	}
}

type LogConfig struct {
	Name         string
	HostDir      string
	ContainerDir string
	Format       string
	FormatConfig map[string]string
	File         string
	Tags         map[string]string
	Target       string
	EstimateTime bool
	Stdout       bool
}

func (p *Pilot) cleanConfigs() error {
	confDir := fmt.Sprintf(p.piloter.ConfHome())
	d, err := os.Open(confDir)
	if err != nil {
		return err
	}
	defer d.Close()

	names, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}

	for _, name := range names {
		path := filepath.Join(confDir, name)
		stat, err := os.Stat(filepath.Join(confDir, name))
		if err != nil {
			return err
		}
		if stat.Mode().IsRegular() {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Pilot) processAllContainers() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	opts := types.ContainerListOptions{}
	containers, err := p.client().ContainerList(context.Background(), opts)
	if err != nil {
		return err
	}

	//clean config
	if err := p.cleanConfigs(); err != nil {
		return err
	}

	containerIDs := make(map[string]string, 0)
	for _, c := range containers {
		if _, ok := containerIDs[c.ID]; !ok {
			containerIDs[c.ID] = c.ID
		}
		if c.State == "removing" {
			continue
		}
		containerJSON, err := p.client().ContainerInspect(context.Background(), c.ID)
		if err != nil {
			return err
		}
		if err = p.newContainer(&containerJSON); err != nil {
			log.Errorf("fail to process container %s: %v", containerJSON.Name, err)
		}
	}
	return p.processAllVolumeSymlink(containerIDs)
}

func (p *Pilot) processAllVolumeSymlink(existingContainerIDs map[string]string) error {
	symlinkContainerIDs := p.listAllSymlinkContainer()
	for containerID := range symlinkContainerIDs {
		if _, ok := existingContainerIDs[containerID]; !ok {
			p.removeVolumeSymlink(containerID)
		}
	}
	return nil
}

func (p *Pilot) listAllSymlinkContainer() map[string]string {
	containerIDs := make(map[string]string, 0)
	linkBaseDir := path.Join(p.base, SYMLINK_LOGS_BASE)
	if _, err := os.Stat(linkBaseDir); err != nil && os.IsNotExist(err) {
		return containerIDs
	}

	projects := listSubDirectory(linkBaseDir)
	for _, project := range projects {
		projectPath := path.Join(linkBaseDir, project)
		services := listSubDirectory(projectPath)
		for _, service := range services {
			servicePath := path.Join(projectPath, service)
			containers := listSubDirectory(servicePath)
			for _, containerID := range containers {
				if _, ok := containerIDs[containerID]; !ok {
					containerIDs[containerID] = containerID
				}
			}
		}
	}
	return containerIDs
}

func listSubDirectory(path string) []string {
	subdirs := make([]string, 0)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return subdirs
	}

	files, err := ioutil.ReadDir(path)
	if err != nil {
		log.Warnf("read %s error: %v", path, err)
		return subdirs
	}

	for _, file := range files {
		if file.IsDir() {
			subdirs = append(subdirs, file.Name())
		}
	}
	return subdirs
}

func putIfNotEmpty(store map[string]string, key, value string) {
	if key == "" || value == "" {
		return
	}
	store[key] = value
}

func container(containerJSON *types.ContainerJSON) map[string]string {
	labels := containerJSON.Config.Labels
	c := make(map[string]string)
	putIfNotEmpty(c, "docker_app", labels[LABEL_PROJECT])
	putIfNotEmpty(c, "docker_app", labels[LABEL_PROJECT_SWARM_MODE])
	putIfNotEmpty(c, "docker_service", labels[LABEL_SERVICE])
	putIfNotEmpty(c, "docker_service", labels[LABEL_SERVICE_SWARM_MODE])
	putIfNotEmpty(c, "k8s_pod", labels[LABEL_POD])
	putIfNotEmpty(c, "k8s_pod_namespace", labels[LABEL_K8S_POD_NAMESPACE])
	putIfNotEmpty(c, "k8s_container_name", labels[LABEL_K8S_CONTAINER_NAME])
	putIfNotEmpty(c, "k8s_node_name", NODE_NAME)

	putIfNotEmpty(c, "docker_container_name", strings.TrimPrefix(containerJSON.Name, "/"))
	putIfNotEmpty(c, "docker_container_created", containerJSON.Created)
	putIfNotEmpty(c, "docker_container_image", containerJSON.Config.Image)
	putIfNotEmpty(c, "docker_container_id", containerJSON.ID)

	putIfNotEmpty(c, "rancher_stack", labels[LABEL_RANCHER_STACK])
	putIfNotEmpty(c, "rancher_stack_service", labels[LABEL_RANCHER_STACK_SERVICE])

	extension(c, containerJSON)
	return c
}

func (p *Pilot) newContainer(containerJSON *types.ContainerJSON) error {
	id := containerJSON.ID
	jsonLogPath := containerJSON.LogPath
	mounts := containerJSON.Mounts
	labels := containerJSON.Config.Labels
	env := containerJSON.Config.Env

	//logConfig.containerDir match types.mountPoint
	/**
	  场景：
	  1. 容器一个路径，中间有多级目录对应宿主机不同的目录
	  2. containerdir对应的目录不是直接挂载的，挂载的是它上级的目录

	  查找：从containerdir开始查找最近的一层挂载
	*/

	container := container(containerJSON)

	for _, e := range env {
		for _, prefix := range p.logPrefix {
			serviceLogs := fmt.Sprintf(ENV_SERVICE_LOGS_TEMPL, prefix)
			if !strings.HasPrefix(e, serviceLogs) {
				continue
			}

			envLabel := strings.SplitN(e, "=", 2)
			if len(envLabel) == 2 {
				labelKey := strings.Replace(envLabel[0], "_", ".", -1)
				labels[labelKey] = envLabel[1]
			}
		}
	}

	logConfigs, err := p.getLogConfigs(jsonLogPath, mounts, labels)
	if err != nil {
		return err
	}

	if len(logConfigs) == 0 {
		log.Debugf("%s has not log config, skip", id)
		return nil
	}

	// create symlink
	p.createVolumeSymlink(containerJSON)

	//pilot.findMounts(logConfigs, jsonLogPath, mounts)
	//生成配置
	logConfig, err := p.render(id, container, logConfigs)
	if err != nil {
		return err
	}
	//TODO validate config before save
	//log.Debugf("container %s log config: %s", id, logConfig)
	if err = ioutil.WriteFile(p.piloter.ConfPathOf(id), []byte(logConfig), os.FileMode(0644)); err != nil {
		return err
	}

	p.tryReload()
	return nil
}

func (p *Pilot) tryReload() {
	select {
	case p.reloadChan <- true:
	default:
		log.Info("Another load is pending")
	}
}

func (p *Pilot) doReload() {
	log.Info("Reload gorouting is ready")
	for {
		<-p.reloadChan
		p.reload()
	}
}

func (p *Pilot) delContainer(id string) error {
	p.removeVolumeSymlink(id)

	// refactor in the future
	if p.piloter.Name() == PILOT_FLUENTD {
		clean := func() {
			log.Infof("Try removing log config %s", id)
			if err := os.Remove(p.piloter.ConfPathOf(id)); err != nil {
				log.Warnf("removing %s log config failure", id)
				return
			}
			p.tryReload()
		}
		time.AfterFunc(15*time.Minute, clean)
		return nil
	} else {
		return p.piloter.OnDestroyEvent(id)
	}
}

func (p *Pilot) client() *client.Client {
	return p.dockerClient
}

func (p *Pilot) processEvent(msg events.Message) error {
	containerId := msg.Actor.ID
	ctx := context.Background()
	switch msg.Action {
	case "start", "restart":
		log.Debugf("Process container start event: %s", containerId)
		if p.exists(containerId) {
			log.Debugf("%s is already exists.", containerId)
			return nil
		}
		containerJSON, err := p.client().ContainerInspect(ctx, containerId)
		if err != nil {
			return err
		}
		return p.newContainer(&containerJSON)
	case "destroy":
		log.Debugf("Process container destory event: %s", containerId)
		err := p.delContainer(containerId)
		if err != nil {
			log.Warnf("Process container destory event error: %s, %s", containerId, err.Error())
		}
	}
	return nil
}

func (p *Pilot) hostDirOf(path string, mounts map[string]types.MountPoint) string {
	confPath := path
	for {
		if point, ok := mounts[path]; ok {
			if confPath == path {
				return point.Source
			} else {
				relPath, err := filepath.Rel(path, confPath)
				if err != nil {
					panic(err)
				}
				return fmt.Sprintf("%s/%s", point.Source, relPath)
			}
		}
		path = filepath.Dir(path)
		if path == "/" || path == "." {
			break
		}
	}
	return ""
}

func (p *Pilot) parseTags(tags string) (map[string]string, error) {
	tagMap := make(map[string]string)
	if tags == "" {
		return tagMap, nil
	}

	kvArray := strings.Split(tags, ",")
	for _, kv := range kvArray {
		arr := strings.Split(kv, "=")
		if len(arr) != 2 {
			return nil, fmt.Errorf("%s is not a valid k=v format", kv)
		}
		key := strings.TrimSpace(arr[0])
		value := strings.TrimSpace(arr[1])
		if key == "" || value == "" {
			return nil, fmt.Errorf("%s is not a valid k=v format", kv)
		}
		tagMap[key] = value
	}
	return tagMap, nil
}

func (p *Pilot) parseLogConfig(name string, info *LogInfoNode, jsonLogPath string, mounts map[string]types.MountPoint) (*LogConfig, error) {
	path := strings.TrimSpace(info.value)
	if path == "" {
		return nil, fmt.Errorf("path for %s is empty", name)
	}

	tags := info.get("tags")
	tagMap, err := p.parseTags(tags)
	if err != nil {
		return nil, fmt.Errorf("parse tags for %s error: %v", name, err)
	}

	target := info.get("target")

	// pol平台index名称不能被强制制定
	// 由logstash生成
	// add default index or topic
	//if _, ok := tagMap["index"]; !ok {
	//	if target != "" {
	//		tagMap["index"] = target
	//	} else {
	//		tagMap["index"] = name
	//	}
	//}

	if _, ok := tagMap["topic"]; !ok {
		if target != "" {
			tagMap["topic"] = target
		} else {
			tagMap["topic"] = name
		}
	}

	format := info.children["format"]
	if format == nil || format.value == "none" {
		format = newLogInfoNode("nonex")
	}

	formatConfig, err := Convert(format)
	if err != nil {
		return nil, fmt.Errorf("in log %s: format error: %v", name, err)
	}

	//特殊处理regex
	if format.value == "regexp" {
		format.value = fmt.Sprintf("/%s/", formatConfig["pattern"])
		delete(formatConfig, "pattern")
	}

	if path == "stdout" {
		logFile := filepath.Base(jsonLogPath)
		if p.piloter.Name() == PILOT_FILEBEAT {
			logFile = logFile + "*"
		}

		return &LogConfig{
			Name:         name,
			HostDir:      filepath.Join(p.base, filepath.Dir(jsonLogPath)),
			File:         logFile,
			Format:       format.value,
			Tags:         tagMap,
			FormatConfig: map[string]string{"time_format": "%Y-%m-%dT%H:%M:%S.%NZ"},
			Target:       target,
			EstimateTime: false,
			Stdout:       true,
		}, nil
	}

	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%s must be absolute path, for %s", path, name)
	}
	containerDir := filepath.Dir(path)
	file := filepath.Base(path)
	if file == "" {
		return nil, fmt.Errorf("%s must be a file path, not directory, for %s", path, name)
	}

	hostDir := p.hostDirOf(containerDir, mounts)
	if hostDir == "" {
		return nil, fmt.Errorf("in log %s: %s is not mount on host", name, path)
	}

	cfg := &LogConfig{
		Name:         name,
		ContainerDir: containerDir,
		Format:       format.value,
		File:         file,
		Tags:         tagMap,
		HostDir:      filepath.Join(p.base, hostDir),
		FormatConfig: formatConfig,
		Target:       target,
	}
	if formatConfig["time_key"] == "" {
		cfg.EstimateTime = true
		cfg.FormatConfig["time_key"] = "_timestamp"
	}
	return cfg, nil
}

type LogInfoNode struct {
	value    string
	children map[string]*LogInfoNode
}

func newLogInfoNode(value string) *LogInfoNode {
	return &LogInfoNode{
		value:    value,
		children: make(map[string]*LogInfoNode),
	}
}

func (node *LogInfoNode) insert(keys []string, value string) error {
	if len(keys) == 0 {
		return nil
	}
	key := keys[0]
	if len(keys) > 1 {
		if child, ok := node.children[key]; ok {
			child.insert(keys[1:], value)
		} else {
			return fmt.Errorf("%s has no parent node", key)
		}
	} else {
		child := newLogInfoNode(value)
		node.children[key] = child
	}
	return nil
}

func (node *LogInfoNode) get(key string) string {
	if child, ok := node.children[key]; ok {
		return child.value
	}
	return ""
}

func (p *Pilot) getLogConfigs(jsonLogPath string, mounts []types.MountPoint, labels map[string]string) ([]*LogConfig, error) {
	var ret []*LogConfig

	mountsMap := make(map[string]types.MountPoint)
	for _, mount := range mounts {
		mountsMap[mount.Destination] = mount
	}

	var labelNames []string
	//sort keys
	for k, _ := range labels {
		labelNames = append(labelNames, k)
	}

	sort.Strings(labelNames)
	root := newLogInfoNode("")
	for _, k := range labelNames {
		for _, prefix := range p.logPrefix {
			serviceLogs := fmt.Sprintf(LABEL_SERVICE_LOGS_TEMPL, prefix)
			if !strings.HasPrefix(k, serviceLogs) || strings.Count(k, ".") == 1 {
				continue
			}

			logLabel := strings.TrimPrefix(k, serviceLogs)
			if err := root.insert(strings.Split(logLabel, "."), labels[k]); err != nil {
				return nil, err
			}
		}
	}

	for name, node := range root.children {
		logConfig, err := p.parseLogConfig(name, node, jsonLogPath, mountsMap)
		if err != nil {
			return nil, err
		}
		ret = append(ret, logConfig)
	}
	return ret, nil
}

func (p *Pilot) exists(containId string) bool {
	if _, err := os.Stat(p.piloter.ConfPathOf(containId)); os.IsNotExist(err) {
		return false
	}
	return true
}

func (p *Pilot) render(containerId string, container map[string]string, configList []*LogConfig) (string, error) {
	for _, config := range configList {
		log.Infof("logs: %s = %v", containerId, config)
	}

	output := os.Getenv(ENV_FLUENTD_OUTPUT)
	if p.piloter.Name() == PILOT_FILEBEAT {
		output = os.Getenv(ENV_FILEBEAT_OUTPUT)
	}

	var buf bytes.Buffer
	context := map[string]interface{}{
		"containerId": containerId,
		"configList":  configList,
		"container":   container,
		"output":      output,
	}

	log.Debugf("context = %s", spew.Sdump(context))
	if err := p.tpl.Execute(&buf, context); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (p *Pilot) reload() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	log.Infof("Reload %s", p.piloter.Name())
	interval := time.Now().Sub(p.lastReload)
	time.Sleep(30*time.Second - interval)
	log.Info("Start reloading")
	err := p.piloter.Reload()
	p.lastReload = time.Now()
	return err
}

func (p *Pilot) createVolumeSymlink(containerJSON *types.ContainerJSON) error {
	if !p.createSymlink {
		return nil
	}

	linkBaseDir := path.Join(p.base, SYMLINK_LOGS_BASE)
	if _, err := os.Stat(linkBaseDir); err != nil && os.IsNotExist(err) {
		if err := os.MkdirAll(linkBaseDir, 0777); err != nil {
			log.Errorf("create %s error: %v", linkBaseDir, err)
		}
	}

	applicationInfo := container(containerJSON)
	containerLinkBaseDir := path.Join(linkBaseDir, applicationInfo["docker_app"],
		applicationInfo["docker_service"], containerJSON.ID)
	symlinks := make(map[string]string, 0)
	for _, mountPoint := range containerJSON.Mounts {
		if mountPoint.Type != mount.TypeVolume {
			continue
		}

		volume, err := p.client().VolumeInspect(context.Background(), mountPoint.Name)
		if err != nil {
			log.Errorf("inspect volume %s error: %v", mountPoint.Name, err)
			continue
		}

		symlink := path.Join(containerLinkBaseDir, volume.Name)
		if _, ok := symlinks[volume.Mountpoint]; !ok {
			symlinks[volume.Mountpoint] = symlink
		}
	}

	if len(symlinks) == 0 {
		return nil
	}

	if _, err := os.Stat(containerLinkBaseDir); err != nil && os.IsNotExist(err) {
		if err := os.MkdirAll(containerLinkBaseDir, 0777); err != nil {
			log.Errorf("create %s error: %v", containerLinkBaseDir, err)
			return err
		}
	}

	for mountPoint, symlink := range symlinks {
		err := os.Symlink(mountPoint, symlink)
		if err != nil && !os.IsExist(err) {
			log.Errorf("create symlink %s error: %v", symlink, err)
		}
	}
	return nil
}

func (p *Pilot) removeVolumeSymlink(containerId string) error {
	if !p.createSymlink {
		return nil
	}

	linkBaseDir := path.Join(p.base, SYMLINK_LOGS_BASE)
	containerLinkDirs, _ := filepath.Glob(path.Join(linkBaseDir, "*", "*", containerId))
	if containerLinkDirs == nil {
		return nil
	}
	for _, containerLinkDir := range containerLinkDirs {
		if err := os.RemoveAll(containerLinkDir); err != nil {
			log.Warnf("remove error: %v", err)
		}
	}
	return nil
}
