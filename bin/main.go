package main

import (
	"flag"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/mcfx0/grass"
)

func main() {
	var configFile string
	flag.StringVar(&configFile, "config", "config.json", "config file name")
	flag.Parse()
	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		log.Fatal(err)
	}
	config, err := grass.UnmarshalConfig(data)
	if err != nil {
		log.Fatal(err)
	}
	client := grass.Client{Config: config}

	err = client.Start()
	if err != nil {
		log.Fatal(err)
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT)
	s := <-ch
	switch s {
	default:
		client.Stop()
	}
}
