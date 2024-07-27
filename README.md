# SubsonicFS
Mount any Subsonic compatible music server as a local directory.

## Usage
```
Usage of subsonicfs:
  -hostname string
    	Hostname/IP Address of the Subsonic Server (default "http://127.0.0.1:4533")
  -mountDir string
    	Location to mount SubsonicFS (default "/tmp/x")
  -password string
    	Password for the account (default "user")
  -passwordAuth
    	Whether or not to use plain-text password authentication (Default is off as it is insecure)
  -username string
    	Username for the account (default "user")
```
Example:
```bash
$ ./subsonicfs -hostname http://127.0.0.1:4533 -mountDir /tmp/SubsonicFS -username demo -password demo
```

## Libraries Used
- FUSE (Filesystem in Userspace)
  - [go-fuse](https://github.com/hanwen/go-fuse)
- [go-subsonic](https://github.com/dweymouth/go-subsonic)

## Tested on:
- [Navidrome](https://www.navidrome.org)
- [Ampache](https://ampache.org)
