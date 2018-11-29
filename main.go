package main

import (
	"9fans.net/go/acme"
	"flag"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	Debug = false
)

var args []string
var win *acme.Win
var needrun = make(chan bool, 1)
var regmatch *regexp.Regexp

func fswatcher(done chan bool, fname string) {
	if Debug {
		log.Println("new watcher: ", fname)
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	err = watcher.Add(fname)
	if err != nil {
		log.Fatal(err)
	}
	waserr := false
	defer func() {
		done <- waserr
	}()
	dir, err := os.Open(fname)
	if err != nil {
		log.Fatal(err)
	}
	names, err := dir.Readdirnames(-1)
	if err != nil {
		log.Fatalf("readdir: %v", err)
	}
	for _, name := range names {
		subfname := fname + "/" + name
		if fi, err := os.Stat(name); err == nil && fi.Mode().IsDir() {
			go fswatcher(done, subfname)
		}
	}
	//I don't remove watchers yet
	for {
		dorun := false
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			switch {
			case event.Op&fsnotify.Create == fsnotify.Create:
				if fi, err := os.Stat(event.Name); err == nil && fi.Mode().IsDir() {
					go fswatcher(done, event.Name)
				}
				dorun = regmatch.MatchString(event.Name)
				if Debug && dorun {
					log.Println("created file:", event.Name)
				}
			case event.Op&fsnotify.Write == fsnotify.Write:
				dorun = regmatch.MatchString(event.Name)
				if Debug && dorun {
					log.Println("modified file:", event.Name)
				}
			}
			if dorun {
				select {
				case needrun <- true:
				default:
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				fmt.Fprintf(os.Stderr, "error:", err)
				return
			}
		}
	}
}

func main() {
	flag.Parse()
	args = flag.Args()
	if len(args) < 2 {
		log.Fatal("usage: Watch regexp command [args]")
	}

	var err error
	regmatch, err = regexp.Compile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "bad regexp")
		log.Fatal("usage: Watch regexp command [args]")
	}
	args = args[1:]

	win, err = acme.New()
	if err != nil {
		log.Fatal(err)
	}
	pwd, _ := os.Getwd()
	win.Name(pwd + "/+watch")
	win.Ctl("clean")
	win.Fprintf("tag", "Get ")
	needrun <- true
	go events()
	go runner()

	done := make(chan bool, 1)
	go fswatcher(done, ".")
	for {
		select {
		case waserr := <-done:
			if waserr {
				fmt.Fprintf(os.Stderr, "watcher exited")
				os.Exit(1)
			}
			break
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func events() {
	for e := range win.EventChan() {
		switch e.C2 {
		case 'x', 'X': // execute
			if string(e.Text) == "Get" {
				select {
				case needrun <- true:
				default:
				}
				continue
			}
			if string(e.Text) == "Del" {
				win.Ctl("delete")
			}
		}
		win.WriteEvent(e)
	}
	os.Exit(0)
}

var run struct {
	sync.Mutex
	id int
}

func runner() {
	var lastcmd *exec.Cmd
	for _ = range needrun {
		run.Lock()
		run.id++
		id := run.id
		run.Unlock()
		if lastcmd != nil {
			lastcmd.Process.Kill()
		}
		lastcmd = nil
		cmd := exec.Command(args[0], args[1:]...)
		r, w, err := os.Pipe()
		if err != nil {
			log.Fatal(err)
		}
		win.Addr(",")
		win.Write("data", nil)
		win.Ctl("clean")
		win.Fprintf("body", "$ %s\n", strings.Join(args, " "))
		cmd.Stdout = w
		cmd.Stderr = w
		if err := cmd.Start(); err != nil {
			r.Close()
			w.Close()
			win.Fprintf("body", "%s: %s\n", strings.Join(args, " "), err)
			continue
		}
		lastcmd = cmd
		w.Close()
		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := r.Read(buf)
				if err != nil {
					break
				}
				run.Lock()
				if id == run.id {
					win.Write("body", buf[:n])
				}
				run.Unlock()
			}
			if err := cmd.Wait(); err != nil {
				run.Lock()
				if id == run.id {
					win.Fprintf("body", "%s: %s\n", strings.Join(args, " "), err)
				}
				run.Unlock()
			}
			win.Fprintf("body", "$\n")
			win.Fprintf("addr", "#0")
			win.Ctl("dot=addr")
			win.Ctl("show")
			win.Ctl("clean")
		}()
	}
}
