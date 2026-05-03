# mediafetch-go

`mediafetch-go` is a lightweight Go package for fetching media metadata and downloading supported videos through `yt-dlp`.

It is designed for apps that need a small in-process media layer without building a full downloader service.

With it, you can:

- validate a supported media URL
- extract title, thumbnail, duration, and available formats
- normalize download options for your UI or API
- download the selected file to disk

This repository is the reusable package only. There is no bundled web app, CLI, or API server.

## Supported Providers

- Facebook
- Instagram
- TikTok
- Twitter/X
- YouTube
- Reddit

## Requirements

Your runtime environment needs:

- Go 1.24 or newer
- `yt-dlp` available on `PATH`
- `ffmpeg` available on `PATH`

## Install

The repo is documented as:

```go
module github.com/Coop25/mediafetch-go
```

Example import:

```go
import mediafetch "github.com/Coop25/mediafetch-go"
```

If you prefer, you can also copy the package files directly into another project as an internal package.

Files needed for that approach:

- `client.go`
- `types.go`
- `providers.go`
- `exec.go`
- `ytdlp_types.go`

## API Overview

Main exported pieces:

- `NewClient`
- `ClientConfig`
- `Client`
- `ValidateSupportedURL`
- `IsFacebookURL`
- `IsInstagramURL`
- `IsTikTokURL`
- `IsTwitterURL`
- `IsYouTubeURL`
- `IsRedditURL`
- `Format`
- `VideoInfo`

## Quick Start

Create a client:

```go
client, err := mediafetch.NewClient(mediafetch.ClientConfig{
	DownloadDir: "downloads",
})
```

Extract metadata:

```go
downloadID, info, err := client.Extract(ctx, videoURL)
```

`Extract` returns:

- a generated download ID
- a `VideoInfo` value
- an error if extraction fails

Download the file:

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

	mediafetch "github.com/Coop25/mediafetch-go"
)

func main() {
	client, err := mediafetch.NewClient(mediafetch.ClientConfig{
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
mediafetch.ValidateSupportedURL(url)
mediafetch.IsFacebookURL(url)
mediafetch.IsInstagramURL(url)
mediafetch.IsTikTokURL(url)
mediafetch.IsTwitterURL(url)
mediafetch.IsYouTubeURL(url)
mediafetch.IsRedditURL(url)
```

## Data Model

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

## How It Works

`mediafetch-go` delegates provider-specific extraction and downloading to `yt-dlp`, while the Go package handles:

- supported-host validation
- metadata parsing
- format normalization
- download directory management
- friendlier application-facing errors

## Repository Layout

```text
.
|-- client.go
|-- exec.go
|-- providers.go
|-- types.go
|-- ytdlp_types.go
|-- go.mod
`-- LICENSE
```

## Notes

- This package depends on upstream `yt-dlp` behavior, so provider changes can break extraction.
- Some videos may fail if they are private, age-restricted, login-protected, or region-limited.
- Facebook links may require a direct video URL instead of a share link.
- Instagram and TikTok support public posts only and may fail on login-gated or restricted content.
- Twitter/X support public posts only and may fail on protected, login-gated, or restricted content.

## Personal Use

- Use this responsibly and in compliance with local law.
- Respect platform terms of service and copyright restrictions.
- Do not redistribute downloaded content without permission.
