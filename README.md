## Hosted on https://zzinstagram.com
You can simply use it within Discord and Telegram by adding "zz" before "instagram" in the post URL. 

<img width="436" height="427" alt="image" src="https://github.com/user-attachments/assets/c73ab8bd-5730-43f6-a6c1-34f9eed3e6ed" />

## Features supported
The following features are supported :
| Subdomain | Post Description | Video handling  | Image Index | Notes |
| --- | :-: | :-: | :-: | --- |
| g.zzinstagram.com |   | ✅ | ✅ | Embed the post without a description |
| d.zzinstagram.com |   | ✅ |   | Embed the post without integration frame |
| n.zzinstagram.com | ✅ | ✅ | ✅ | The normal way to embed posts (description + username) |

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
