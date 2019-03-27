/*
Copyright 2019 Greg Burek.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"mime"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"

	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	_ "github.com/mattn/go-sqlite3"
	"gopkg.in/urfave/cli.v1"
)

// Gif representing db gif entry
type Gif struct {
	id                                        int
	filename, url, hash, newURL, fullfilename string
}

var (
	version string
)

func s3Uploader(region string) *s3manager.Uploader {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region)},
	)

	if err != nil {
		log.Fatal(err)
	}

	return s3manager.NewUploader(sess)
}

func s3Client(region string) *s3.S3 {
	// Initialize a session in us-west-2 that the SDK will use to load
	// credentials from the shared credentials file ~/.aws/credentials.
	return s3.New(session.New(), &aws.Config{
		Region: aws.String(region)},
	)
}

// Exists checks if a file exists
func Exists(filename string) bool {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return false
	}

	return true
}

func hashFiles(filePath string) (string, error) {
	//Initialize variable returnString now in case an error has to be returned
	var returnString string

	//Open the passed argument and check for any error
	file, err := os.Open(filePath)
	if err != nil {
		return returnString, err
	}

	//Tell the program to call the following function when the current function returns
	defer file.Close()

	//Open a new hash interface to write to
	hash := md5.New()

	//Copy the file in the hash interface and check for any error
	if _, err := io.Copy(hash, file); err != nil {
		return returnString, err
	}

	//Get the 16 bytes hash
	hashInBytes := hash.Sum(nil)

	//Convert the bytes to a string
	returnString = hex.EncodeToString(hashInBytes)

	return returnString, nil
}

func upsertBucket(svc *s3.S3, bucketName string) {
	fmt.Printf("Creating bucket %v\n", bucketName)
	_, err := svc.CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeBucketAlreadyExists:
				log.Fatal(s3.ErrCodeBucketAlreadyExists, aerr.Error())
			case s3.ErrCodeBucketAlreadyOwnedByYou:
				fmt.Printf("Bucket %v already exists\n", bucketName)
				return
			default:
				log.Fatal(aerr.Error())
			}
		} else {
			log.Fatal(err)
		}
	}
	fmt.Printf("Done\n")
}

func setBucketPublic(svc *s3.S3, bucketName string) {
	fmt.Printf("Setting bucket %v for public-read so others can enjoy your gifs\n", bucketName)
	acl := "public-read"
	_, err := svc.PutBucketAcl(&s3.PutBucketAclInput{
		Bucket: &bucketName,
		ACL:    &acl,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Done\n")
}

func setBucketEncryption(svc *s3.S3, bucketName string) {
	fmt.Printf("Checking bucket %v for default encryption\n", bucketName)
	input := &s3.GetBucketEncryptionInput{Bucket: aws.String(bucketName)}
	_, err := svc.GetBucketEncryption(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case "ServerSideEncryptionConfigurationNotFoundError":
				fmt.Printf("Bucket %v has no default encryption\n", bucketName)
				fmt.Printf("Applying default AES256 encryption to bucket\n")

				defEnc := &s3.ServerSideEncryptionByDefault{SSEAlgorithm: aws.String(s3.ServerSideEncryptionAes256)}
				rule := &s3.ServerSideEncryptionRule{ApplyServerSideEncryptionByDefault: defEnc}
				rules := []*s3.ServerSideEncryptionRule{rule}
				serverConfig := &s3.ServerSideEncryptionConfiguration{Rules: rules}

				_, error := svc.PutBucketEncryption(&s3.PutBucketEncryptionInput{Bucket: aws.String(bucketName), ServerSideEncryptionConfiguration: serverConfig})
				if error != nil {
					log.Fatal(err)
				}
				fmt.Printf("Done\n")
				return
			default:
				log.Fatal(aerr.Error())
			}
		} else {
			log.Fatal(err)
		}
	}
	fmt.Printf("Bucket %v already has some kind of default encryption\n", bucketName)
}

func getCannonicalGifwitDir(gifwitDir string) string {
	usr, _ := user.Current()
	dir := usr.HomeDir

	// Check in case of paths like "/something/~/something/"
	if gifwitDir[:2] == "~/" {
		gifwitDir = filepath.Join(dir, gifwitDir[2:])
	}

	return gifwitDir
}

func openGifwitDb(db *sql.DB, bucket string, c chan Gif) {
	rows, err := db.Query("select Z_PK, ZCACHE_FILE, ZURL from ZIMAGE where ZDOWNLOADED=1 AND ZURL NOT LIKE $1 order by Z_PK", "%"+bucket+"%")
	checkErr(err)

	defer rows.Close()

	for rows.Next() {
		var id int
		var filename, url string
		err := rows.Scan(&id, &filename, &url)
		checkErr(err)

		fmt.Printf("Placing gif %v on hashChan\n", filename)
		c <- Gif{id: id, filename: filename, url: url}
	}

	err = rows.Err()
	checkErr(err)

	close(c)
}

func hashGifs(wg *sync.WaitGroup, canonicalGifwitDir string, hashChan chan Gif, uploadChan chan Gif) {
	defer wg.Done()

	for gif := range hashChan {
		fmt.Printf("Got gif %v from hashChan\n", gif.filename)
		var imageFilename = filepath.Join(canonicalGifwitDir, gif.filename)
		if Exists(imageFilename) {
			hash, err := hashFiles(imageFilename)
			if err != nil {
				log.Fatal(err)
			}

			gif.hash = hash
			gif.fullfilename = imageFilename
			fmt.Printf("Placing gif %v on uploadChan\n", gif)
			uploadChan <- gif
		}
	}
}

// Guess image format from gif/jpeg/png
func guessImageFormat(r io.Reader) (format string, err error) {
	_, format, err = image.DecodeConfig(r)
	return
}

func guessImageMimeTypes(r io.Reader) string {
	format, _ := guessImageFormat(r)
	if format == "" {
		return ""
	}
	return mime.TypeByExtension("." + format)
}

func checkAndUploadGif(wg *sync.WaitGroup, region, bucket string, uploadChan chan Gif, saveChan chan Gif) {
	defer wg.Done()

	svc := s3Client(region)
	svcUpload := s3Uploader(region)
	for gif := range uploadChan {
		f, err := os.Open(gif.fullfilename)
		if err != nil {
			fmt.Printf("failed to open file %q, %v", gif.filename, err)
		}

		mimeType := guessImageMimeTypes(f)
		_, _ = f.Seek(0, 0)
		format, _ := guessImageFormat(f)
		_, _ = f.Seek(0, 0)

		remoteFilename := strings.Join([]string{gif.hash, format}, ".")

		var location string

		if exists := objExists(svc, bucket, remoteFilename); exists {
			fmt.Printf("file %v exists\n", remoteFilename)
			params := &s3.GetObjectInput{
				Bucket: aws.String(bucket),
				Key:    aws.String(remoteFilename),
			}

			_, _ = svc.GetObjectRequest(params)

			location = "https://s3-" + region + ".amazonaws.com/" + bucket + "/" + remoteFilename
		} else {
			location = uploadGif(svcUpload, bucket, remoteFilename, mimeType, f)
		}

		gif.newURL = location
		saveChan <- gif
	}
}

func objExists(svc *s3.S3, bucket, remoteFilename string) bool {
	input := &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(remoteFilename),
	}

	_, err := svc.HeadObject(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case "NotFound":
				return false
			default:
				fmt.Println(aerr.Error())
			}
		} else {
			// Print the error, cast err to awserr.Error to get the Code and
			// Message from an error.
			fmt.Println(err.Error())
		}
	}

	return true
}

func uploadGif(uploader *s3manager.Uploader, bucket, remoteFilename, mimeType string, f io.Reader) string {
	acl := "public-read"
	result, err := uploader.Upload(&s3manager.UploadInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(remoteFilename),
		Body:        f,
		ACL:         &acl,
		ContentType: &mimeType,
	})
	if err != nil {
		fmt.Printf("failed to upload file, %v", err)
	}
	fmt.Printf("file uploaded to, %v\n", result.Location)
	return result.Location
}

func cleanUpChan(wg *sync.WaitGroup, c chan Gif) {
	wg.Wait()
	close(c)
}

func main() {
	var gifwitDir string
	var bucketName string
	var region string

	app := cli.NewApp()
	app.Name = "durablewit"
	app.Usage = "Make your gifwit library durable by uploading to s3"
	app.Version = version

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:        "dir, d",
			Value:       "~/Library/Containers/stevesmith.gifwit/Data/Library/Application Support/stevesmith.gifwitfiles/",
			Usage:       "gifwit directory path with gifs and gifwit.storedata DB",
			Destination: &gifwitDir,
		},
		cli.StringFlag{
			Name:        "bucket, b",
			Value:       "durablewit",
			Usage:       "name of bucket to be created and made publicly readable to upload gifs to",
			Destination: &bucketName,
		},
		cli.StringFlag{
			Name:        "region, r",
			Value:       "us-west-2",
			Usage:       "AWS S3 region to use",
			Destination: &region,
		},
	}

	app.Action = func(c *cli.Context) error {
		svc := s3Client(region)
		upsertBucket(svc, bucketName)
		//setBucketPublic(svc, bucketName)
		setBucketEncryption(svc, bucketName)

		canonicalGifwitDir := getCannonicalGifwitDir(gifwitDir)

		dbFile := filepath.Join(canonicalGifwitDir, "gifwit.storedata?_busy_timeout=5000")

		hashChan := make(chan Gif, 1000)
		uploadChan := make(chan Gif, 1000)
		saveChan := make(chan Gif, 1000)

		var hashwg sync.WaitGroup
		var uploadwg sync.WaitGroup
		var savewg sync.WaitGroup

		for i := 0; i < 10; i++ {
			hashwg.Add(1)
			go hashGifs(&hashwg, canonicalGifwitDir, hashChan, uploadChan)

			uploadwg.Add(1)
			go checkAndUploadGif(&uploadwg, region, bucketName, uploadChan, saveChan)
		}

		go cleanUpChan(&hashwg, uploadChan)
		go cleanUpChan(&uploadwg, saveChan)

		savewg.Add(1)

		pool, err := sql.Open("sqlite3", dbFile)
		checkErr(err)
		defer pool.Close()

		pool.SetConnMaxLifetime(0)
		pool.SetMaxIdleConns(1)
		pool.SetMaxOpenConns(1)

		go writeNewURLToGifWit(&savewg, pool, saveChan)

		openGifwitDb(pool, bucketName, hashChan)

		savewg.Wait()
		fmt.Println("Main: Completed")
		return nil
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func checkErr(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func writeNewURLToGifWit(wg *sync.WaitGroup, db *sql.DB, c chan Gif) {
	defer wg.Done()

	stmt, err := db.Prepare("UPDATE ZIMAGE SET ZURL=? WHERE Z_PK=?")
	checkErr(err)

	for gif := range c {
		res, err := stmt.Exec(gif.newURL, gif.id)
		checkErr(err)

		affect, err := res.RowsAffected()
		checkErr(err)

		fmt.Println(affect)
	}

}
