package main

import (
	log "github.com/Sirupsen/logrus"
	"github.com/diablowu/log-pilot/pilot"
	"io/ioutil"
	"os"
	"github.com/alecthomas/kingpin"
)

const DEFUALT_VERSION = "0.1"

func main() {

	app := kingpin.New("log-pilot", "collect loggger file from docker host")

	app.Version(DEFUALT_VERSION)

	// 模板路径
	template := app.Flag("template", "Template filepath for fluentd or filebeat.").Short('t').Required().ExistingFile()

	// 主机文件系统挂在到容器内的路径，默认为 /host
	baseDir := app.Flag("base", "Directory which mount host root.").Default("/host").Short('b').ExistingDir()

	// 日志级别
	level := app.Flag("log", "Log level").Default("info").Short('v').Enum("panic", "fatal", "error", "warn", "info", "debug")

	dry := app.Flag("dryrun", "Dry run.").Short('d').Default("false").Bool()

	kingpin.MustParse(app.Parse(os.Args[1:]))

	log.SetOutput(os.Stdout)

	// 不会error
	logLevel, _ := log.ParseLevel(*level)
	log.SetLevel(logLevel)

	if !*dry {
		b, err := ioutil.ReadFile(*template)
		if err != nil {
			log.Panic(err)
		}

		log.Fatal(pilot.Run(string(b), *baseDir))
	}

}
