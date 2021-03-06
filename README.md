# durablewit
[![Build Status](https://travis-ci.org/gregburek/durablewit.svg?branch=master)](https://travis-ci.org/gregburek/durablewit)

Images found online are not stable archives. Blog redesigns, bot takedowns,
startup failures and general link rot mean saved links will not resolve when
you would love them to. GIF link management apps like [gifwit](http://gifwit.com/) at
least save the images locally, but do not provide a way to re-upload and save
the new link for immediate or later use.

This project:

1. Takes a dir of gifwit data with images and gifwit.storedata
2. Checks for and optionally creates a public S3 bucket to upload images to
3. Uploads all images in the gifwit db to the S3 bucket
4. Updates the gifwit db with new, durable links

After running durablewit, the gifwit db is clean and an archive can be shared
with other folks or migrated to a new computer, as the links are valid and
working.

The S3 object names are MD5 hashes of the files, which assist in
de-duplication.

The durablewit project is written in very bad Go, with a dir and build
template taken partially from [thockin/go-build-template](https://github.com/thockin/go-build-template).

Go was chosen as a learning experience and on the chance that go routine
concurrency would be a good fit for a embarrassingly parallel workload, like
hashing and uploading several GB of files. FWIW this code is able to saturate
an 100 mbps link while uploading files to S3, so (ﾉ･ｪ･)ﾉ

![Chris Farley: Remember that time I posted that cool GIF and everyone thought I was really funny? Jeff Daniels: Yeah, I remember. CF: That was awesome.](https://durablewit.s3.us-west-2.amazonaws.com/7278f77ccff00330bff7429cdfe17854.gif)

## Running

```bash
$ bin/durablewit -h
NAME:
   durablewit - Make your gifwit library durable by uploading to s3

USAGE:
   durablewit [global options] command [command options] [arguments...]

VERSION:
   v0.1

COMMANDS:
     help, h  Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --dir value, -d value     gifwit directory path with gifs and gifwit.storedata DB (default: "~/Library/Containers/stevesmith.gifwit/Data/Library/Application Support/stevesmith.gifwitfiles/")
   --bucket value, -b value  name of bucket to be created and made publicly readable to upload gifs to (default: "durablewit")
   --region value, -r value  AWS S3 region to use (default: "us-west-2")
   --help, -h                show help
   --version, -v             print the version
```

## Building

Run `make` or `make build` to compile your app.

Run `make clean` to clean up.
