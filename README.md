## Hosted on https://zzinstagram.com
You can simply use it within Discord and Telegram by adding "zz" before "instagram" in the post URL. 
<img width="436" height="427" alt="image" src="https://github.com/user-attachments/assets/c73ab8bd-5730-43f6-a6c1-34f9eed3e6ed" />

## Features supported
Currently, it only supports the `Gallery mode` (removes the post description from the embedding) from [Instafix](https://github.com/Wikidepia/InstaFix/) resolvers. 
<br>
You can use it through `g.zzinstagram.com` (simply add g. before zzinstagram.com)

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
