## Hosted on https://zzinstagram.com
You can simply use it within Discord and Telegram by adding "zz" before "instagram" in the post URL. 
<img width="436" height="427" alt="image" src="https://github.com/user-attachments/assets/c73ab8bd-5730-43f6-a6c1-34f9eed3e6ed" />

## Features supported
The following features are supported :
- Gallery mode - i.e, removes the post description from the embedding - available through the subdomain https://g.zzinstagram.com
- Direct mode - embed the video **only** - available through https://d.zzinstagram.com *tip: this one usually works best for instagram reels*
- Normal mode - keep the description and the account's name in the embedding - available through https://n.zzinstagram.com
- Image index - choose a specific image from the carousel of an instagram post by adding ?image_index=<index> at the end of the URL (compatible with the normal mode and the gallery mode)
<br>

## How to build 
Make sure you have [Golang](https://go.dev/) installed, and run `go build .` within the project directory.

## Usage
Execute with:
```bash
./InstagramEmbedResolver -p [port]
```
Default listening port is `8080` if none is specified.

## Credits
As this app is only acting as a proxy, it relies on other instagram embedding softwares such as [Instafix](https://github.com/Wikidepia/InstaFix/) and [vxinstagram](https://github.com/Lainmode/InstagramEmbed-vxinstagram).
