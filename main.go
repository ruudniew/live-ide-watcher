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
	Path string `json:"path"`
	Name string `json:"name"`
	Directories []*Directory `json:"directories"`
	Files []*File `json:"files"`
	Open bool `json:"open"`
}

type File struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Code string `json:"code"`
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func (r *http.Request) bool {
		return true
	},
}

func readFiles(dir *Directory, files []os.FileInfo) {
	for _, file := range files {
		// If a file is found
		if !file.IsDir() {
			fileContent, err := ioutil.ReadFile(dir.Path + "/" + file.Name())

			if  err != nil {
				log.Printf("could not read the file content: %+v", err)
				return
			}

			dir.Files = append(dir.Files, &File {
				Name: file.Name(),
				Path: dir.Path + "/" + file.Name(),
				Code: string(fileContent),
			})

		}

		// Directory found
		if file.IsDir() {
			dir.Directories = append(dir.Directories, &Directory {
				Name: file.Name(),
				Path: dir.Path + "/" + file.Name(),
				Directories: make([]*Directory, 0),
				Files: make([]*File, 0),
				Open: false,
			})
		}
	}

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

func main () {
	var err error
	var conn *websocket.Conn

	http.HandleFunc("/", func (w http.ResponseWriter, r *http.Request) {
		conn, err = upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println(err)
			return
		}
	})

	go http.ListenAndServe(":3600", nil)

	rootDirPath := "."

	rootDir := &Directory {
		Name: "bigbrother",
		Path: rootDirPath,
		Directories: make([]*Directory, 0),
		Files: make([]*File, 0),
		Open: false,
	}

	files, err := ioutil.ReadDir(rootDirPath)
	if err != nil {
		log.Fatal(err)
	}

	// After this point, rootDir contains all files and directories in this folder
	readFiles(rootDir, files)

	// Initialize the watcher
	w := watcher.New()

	// Maximum one event per cycle of 100 ms
	w.SetMaxEvents(1)

	go func() {
		for {
			select {
			case event := <-w.Event:
				if event.IsDir() {
					if !strings.Contains(event.Path, "bigbrother/") {
						if conn != nil {
							err = conn.WriteJSON(rootDir)

							if err != nil {
								log.Fatalf("couldn't notify the websocket of changes: %+v", err)
							}
						}

					} else {
						relativePath := event.Path[strings.Index(event.Path, "bigbrother/")+11:]

						changedDir := &Directory {
							Path: relativePath,
							Name: event.Name(),
							Directories: make([]*Directory, 0),
							Files: make([]*File, 0),
							Open: false,
						}


						files, err := ioutil.ReadDir(relativePath)

						if err != nil {
							log.Fatalf("couldn't read files in relative path: %+v", err)
						}

						readFiles(changedDir, files)

						if changedDir.Path != rootDir.Path {
							swapDirectory(rootDir, changedDir)
						}

						if conn != nil {
							err = conn.WriteJSON(rootDir)

							if err != nil {
								log.Fatalf("couldn't notify the websocket of changes: %+v", err)
							}
						}
					}
				}

			case err := <-w.Error:
				log.Fatalln(err)
			case <-w.Closed:
				return
			}
		}
	}()

	// Watch test_folder recursively for changes.
	if err := w.AddRecursive("."); err != nil {
		log.Fatalln(err)
	}

	// Start the watching process - it'll check for changes every 100ms.
	if err := w.Start(time.Millisecond * 100); err != nil {
		log.Fatalln(err)
	}
}
