package main

import (
	"github.com/gorilla/websocket"
	"github.com/radovskyb/watcher"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type Directory struct {
	Path        string       `json:"path"`
	Name        string       `json:"name"`
	Directories []*Directory `json:"directories"`
	Files       []*File      `json:"files"`
	Open        bool         `json:"open"`
}

type File struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Code string `json:"code"`
}

// Websocket connection
var conn *websocket.Conn

func main() {
	// Start listener
	go startWebsocket()

	// Configure the root directory
	rootDirPath := os.Args[1] // provide the path in the first argument when starting the watcher
	rootDirName := os.Args[2] // provide the name of the root directory as the second argument, when starting the watcher
	rootDir := Directory{
		Name:        rootDirName,
		Path:        rootDirPath,
		Directories: make([]*Directory, 0),
		Files:       make([]*File, 0),
		Open:        false,
	}

	// Read the files in the root directory
	files, err := ioutil.ReadDir(rootDirPath)
	if err != nil {
		log.Fatal(err)
	}

	// After this point, the rootDir variable contains all files and directories in this folder
	readFiles(&rootDir, files)

	// Now watch for changes
	startWatcher(&rootDir)
}

func readFiles(dir *Directory, files []os.FileInfo) {
	for _, file := range files {
		// If a file is found
		if !file.IsDir() {
			fileContent, err := ioutil.ReadFile(dir.Path + "/" + file.Name())

			if err != nil {
				log.Printf("could not read the file content: %+v", err)
				return
			}

			dir.Files = append(dir.Files, &File{
				Name: file.Name(),
				Path: dir.Path + "/" + file.Name(),
				Code: string(fileContent),
			})

		}

		// Directory found
		if file.IsDir() {
			dir.Directories = append(dir.Directories, &Directory{
				Name:        file.Name(),
				Path:        dir.Path + "/" + file.Name(),
				Directories: make([]*Directory, 0),
				Files:       make([]*File, 0),
				Open:        false,
			})
		}
	}

	// Loop through the found directories and recursively read their files
	for _, subDir := range dir.Directories {
		subDirFiles, err := ioutil.ReadDir(subDir.Path)
		if err != nil {
			log.Fatal(err)
		}

		readFiles(subDir, subDirFiles)
	}
}

func swapDirectory(rootDir *Directory, changedDir *Directory) {
	lookForPath := changedDir.Path
	log.Printf("lookForPath: %s", lookForPath)

	directoriesToOpen := strings.Split(lookForPath, "/")

	var foundDirParent *Directory
	foundDir := rootDir
	for _, directoryToOpen := range directoriesToOpen {
		for _, dir := range foundDir.Directories {
			if dir.Name == directoryToOpen {
				foundDirParent = foundDir
				foundDir = dir
			}
		}
	}

	if foundDirParent == nil {
		log.Printf("did not find parent. Please fix")
		return
	}

	for i, subDir := range foundDirParent.Directories {
		if subDir.Name == changedDir.Name {
			foundDirParent.Directories[i] = changedDir
		}
	}
}

func startWebsocket() {
	var err error

	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, err = upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println(err)
			return
		}
	})

	go http.ListenAndServe(":3600", nil)
}

func startWatcher(rootDir *Directory) {
	var err error

	w := watcher.New()
	w.SetMaxEvents(1) // Maximum one event per cycle of 100 ms

	go func() {
		for {
			select {
			case event := <-w.Event:
				if event.IsDir() {
					if conn == nil {
						log.Printf("there's no websocket connection right now")
						continue
					}

					if !strings.Contains(event.Path, rootDir.Name) {
						err = conn.WriteJSON(rootDir)
						if err != nil {
							log.Printf("couldn't notify the websocket of changes: %+v", err)
						}
						continue
					}

					// Default: The rootDir itself changed
					relativePath := rootDir.Path

					// rootDir itself didn't change
					eventDirs := strings.Split(relativePath, "/")
					if eventDirs[len(eventDirs)-1] != rootDir.Path {
						relativePath = event.Path[strings.Index(event.Path, rootDir.Name+"/")+len(rootDir.Name)+1:]
					}

					changedDir := &Directory{
						Path:        relativePath,
						Name:        event.Name(),
						Directories: make([]*Directory, 0),
						Files:       make([]*File, 0),
						Open:        false,
					}

					files, err := ioutil.ReadDir(relativePath)

					if err != nil {
						log.Fatalf("couldn't read files in relative path: %+v", err)
					}

					readFiles(changedDir, files)

					if changedDir.Path != rootDir.Path {
						swapDirectory(rootDir, changedDir) // swap out the updated directory
					} else {
						rootDir = changedDir
					}

					err = conn.WriteJSON(rootDir)
					if err != nil {
						log.Fatalf("couldn't notify the websocket of changes: %+v", err)
					}
				}

			case err := <-w.Error:
				log.Fatalln(err)
			case <-w.Closed:
				return
			}
		}
	}()

	// Watch rootDir recursively for changes.
	if err := w.AddRecursive(rootDir.Path); err != nil {
		log.Fatalln(err)
	}

	// Start the watching process - it'll check for changes every 100ms.
	if err := w.Start(time.Millisecond * 100); err != nil {
		log.Fatalln(err)
	}
}
