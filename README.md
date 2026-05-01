# videodl

`videodl` is a small Go package for working with video URLs through `yt-dlp`.

It gives your application a direct in-process way to:

- accept a supported URL
- inspect basic video metadata
- inspect normalized download formats
- download the selected file to disk

This repository is intentionally just the reusable package. There is no web app, API server, or frontend here.

## Supported Sites

- Facebook
- YouTube
- Reddit

## Requirements

Your runtime environment needs:

- Go 1.24 or newer
- `yt-dlp` available on `PATH`
- `ffmpeg` available on `PATH`

## Install Or Copy

You can use this code in either of two ways:

1. Import it as a Go module
2. Copy the `.go` files into your own project and keep them as an internal package

If you copy it into another project, the only source files you need are:

- `client.go`
- `types.go`
- `providers.go`
- `exec.go`
- `ytdlp_types.go`

## Package API

Main exported pieces:

- `NewClient`
- `ClientConfig`
- `Client`
- `ValidateSupportedURL`
- `IsFacebookURL`
- `IsYouTubeURL`
- `IsRedditURL`
- `Format`
- `VideoInfo`

### Create a Client

```go
client, err := videodl.NewClient(videodl.ClientConfig{
	DownloadDir: "downloads",
})
```

### Extract Metadata

```go
downloadID, info, err := client.Extract(ctx, videoURL)
```

`Extract` returns:

- a generated download ID
- a `VideoInfo` value
- an error, if extraction fails

### Download the File

```go
filePath, err := client.Download(ctx, videoURL, downloadID, "best")
```

`Download` returns the local file path written by `yt-dlp`.

## Example

```go
package main

import (
	"context"
	"log"

	videodl "facebook-video-downloader"
)

func main() {
	client, err := videodl.NewClient(videodl.ClientConfig{
		DownloadDir: "downloads",
	})
	if err != nil {
		log.Fatal(err)
	}

	url := "https://youtu.be/dQw4w9WgXcQ"

	downloadID, info, err := client.Extract(context.Background(), url)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("title:", info.Title)
	log.Println("formats:", len(info.Formats))

	filePath, err := client.Download(context.Background(), url, downloadID, "best")
	if err != nil {
		log.Fatal(err)
	}

	log.Println("saved to:", filePath)
}
```

## URL Helpers

Use the helpers when you want to check support before extraction:

```go
videodl.ValidateSupportedURL(url)
videodl.IsFacebookURL(url)
videodl.IsYouTubeURL(url)
videodl.IsRedditURL(url)
```

## Data Shapes

`VideoInfo` contains:

- `ThumbnailURL`
- `Title`
- `Duration`
- `Formats`

Each `Format` contains:

- `FormatID`
- `Resolution`
- `Ext`
- `Filesize`
- `FormatNote`

## File Layout

```text
.
├── client.go
├── exec.go
├── providers.go
├── types.go
├── ytdlp_types.go
├── go.mod
└── LICENSE
```

## Notes
## Personal Use

- This tool is for personal use only.
- Please respect copyright laws.
- Please respect Facebook's Terms of Service.
- Downloaded videos should not be distributed without permission.

- This package delegates site-specific extraction to `yt-dlp`.
- Some URLs may fail because the upstream site changes behavior.
- Private, age-restricted, login-protected, or region-limited videos may not download successfully.
