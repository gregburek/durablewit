package main

import (
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
	"gopkg.in/urfave/cli.v1"
)

func main() {
	var gifwitDir string

	app := cli.NewApp()
	app.Name = "durablewit"
	app.Usage = "Make your gifwit library durable by uploading to s3"

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:        "dir, d",
			Value:       "~/Library/Containers/stevesmith.gifwit/Data/Library/Application Support/stevesmith.gifwitfiles/",
			Usage:       "gifwit directory path with gifs and gifwit.storedata DB",
			Destination: &gifwitDir,
		},
	}

	app.Action = func(c *cli.Context) error {
		usr, _ := user.Current()
		dir := usr.HomeDir

		// Check in case of paths like "/something/~/something/"
		if gifwitDir[:2] == "~/" {
			gifwitDir = filepath.Join(dir, gifwitDir[2:])
		}

		dbFile := filepath.Join(gifwitDir, "gifwit.storedata")

		db, err := sql.Open("sqlite3", dbFile)
		if err != nil {
			log.Fatal(err)
		}
		defer db.Close()

		rows, err := db.Query("select Z_PK, ZCACHE_FILE, ZURL from ZIMAGE where ZDOWNLOADED=1")
		if err != nil {
			log.Fatal(err)
		}
		defer rows.Close()

		for rows.Next() {
			var id int
			var filename, url string
			err = rows.Scan(&id, &filename, &url)
			if err != nil {
				log.Fatal(err)
			}

			var imageFilename = filepath.Join(gifwitDir, filename)

			if Exists(imageFilename) {
				hash, err := hashFilesMd5(imageFilename)
				if err != nil {
					log.Fatal(err)
				}

				fmt.Println(id, filename, url, hash)
			}
		}
		err = rows.Err()
		if err != nil {
			log.Fatal(err)
		}

		fmt.Printf("Hello %q", gifwitDir)

		return nil
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

// Exists checks if a file exists
func Exists(filename string) bool {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return false
	}

	return true
}

func hashFilesMd5(filePath string) (string, error) {
	//Initialize variable returnMD5String now in case an error has to be returned
	var returnMD5String string

	//Open the passed argument and check for any error
	file, err := os.Open(filePath)
	if err != nil {
		return returnMD5String, err
	}

	//Tell the program to call the following function when the current function returns
	defer file.Close()

	//Open a new hash interface to write to
	hash := md5.New()

	//Copy the file in the hash interface and check for any error
	if _, err := io.Copy(hash, file); err != nil {
		return returnMD5String, err
	}

	//Get the 16 bytes hash
	hashInBytes := hash.Sum(nil)[:16]

	//Convert the bytes to a string
	returnMD5String = hex.EncodeToString(hashInBytes)

	return returnMD5String, nil
}
