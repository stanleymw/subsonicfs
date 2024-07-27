package main

import (
	"context"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"sync"
	"syscall"

	"github.com/dweymouth/go-subsonic/subsonic"
	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

var hasher = fnv.New64()
var SubsonicClient subsonic.Client

func hash(s string) uint64 {
	hasher.Reset()
	hasher.Write([]byte(s))
	return hasher.Sum64()
}

type subsonicFS struct {
	fs.Inode
}

type subsonicArtist struct {
	fs.Inode

	clientObj *subsonic.ArtistID3
}

type subsonicAlbum struct {
	fs.Inode

	clientObj *subsonic.AlbumID3
}

type subsonicSong struct {
	fs.Inode

	clientObj *subsonic.Child
	streamer  io.Reader
	readLock  sync.Mutex
}

// The root populates the tree in its OnAdd method
var _ = (fs.NodeOnAdder)((*subsonicFS)(nil))

func (song *subsonicSong) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	if song.streamer != nil {
		return &song, fuse.FOPEN_NONSEEKABLE, 0
	}

	stmr, err := SubsonicClient.Stream(song.clientObj.ID, nil)
	if err != nil {
		return nil, 0, syscall.ENOENT
	}

	song.readLock.Lock()
	song.streamer = stmr
	song.readLock.Unlock()

	return &song, fuse.FOPEN_NONSEEKABLE, 0
}

var last int64

func (song *subsonicSong) Read(ctx context.Context, f fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	song.readLock.Lock()
	defer song.readLock.Unlock()

	if song.streamer == nil {
		return nil, syscall.EIO
	}

	// log.Printf("[read] offset: %d | amt: %d | delta: %d", off, len(dest), off-last)
	// last = off

	io.ReadFull(song.streamer, dest)
	return fuse.ReadResultData(dest), 0
}
func (song *subsonicSong) Flush(ctx context.Context, f fs.FileHandle) syscall.Errno {
	song.readLock.Lock()
	song.streamer = nil
	song.readLock.Unlock()

	return 0
}
func (song *subsonicSong) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	sobj := song.clientObj

	out.Ctime = uint64(sobj.Created.Unix())
	out.Mtime = uint64(sobj.Created.Unix())
	out.Atime = uint64(sobj.Played.Unix())

	out.Mode = syscall.S_IFREG
	out.Size = uint64(sobj.Size)
	out.Ino = song.StableAttr().Ino

	return 0
}

func (album *subsonicAlbum) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	aobj := album.clientObj

	out.Ctime = uint64(aobj.Created.Unix())
	out.Mtime = uint64(aobj.Created.Unix())
	out.Atime = uint64(aobj.Created.Unix())

	out.Mode = syscall.S_IFREG
	out.Ino = album.StableAttr().Ino

	return 0
}

// Album -> Song (dynamic discovery)
func (album *subsonicAlbum) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	// if we havent already discovered it yet
	if len(album.Children()) == 0 {
		albumInfo, _ := SubsonicClient.GetAlbum(album.clientObj.ID)
		for _, song := range albumInfo.Song {
			songIno := album.Inode.NewPersistentInode(
				ctx,
				&subsonicSong{
					clientObj: song,
				},
				fs.StableAttr{Mode: syscall.S_IFREG, Ino: hash(song.ID)},
			)
			album.Inode.AddChild(path.Base(song.Path), songIno, true)
		}
	}

	songs := []fuse.DirEntry{}
	for name, ino := range album.Children() {
		songs = append(songs, fuse.DirEntry{
			Mode: fuse.S_IFREG,
			Name: name,
			Ino:  ino.StableAttr().Ino,
		})
	}

	return fs.NewListDirStream(songs), 0
}

// Artist -> Album (Dynamic discovery)
func (artist *subsonicArtist) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	// if we havent already discovered it yet
	if len(artist.Children()) == 0 {
		art2, _ := SubsonicClient.GetArtist(artist.clientObj.ID)
		for _, album := range art2.Album {
			// dir, base := filepath.Split(f.Name)
			// ch := p.NewPersistentInode(ctx, &fs.Inode{}, fs.StableAttr{Mode: fuse.S_IFDIR})
			// p.AddChild(f.Title, ch, true)
			albumInode := artist.NewPersistentInode(
				ctx,
				&subsonicAlbum{
					clientObj: album,
				},
				fs.StableAttr{Mode: fuse.S_IFDIR, Ino: hash(album.ID)})
			artist.AddChild(fmt.Sprint(album.Name, " (", album.Year, ")"), albumInode, true)
		}
	}

	songs := []fuse.DirEntry{}
	for name, ino := range artist.Children() {
		songs = append(songs, fuse.DirEntry{
			Mode: fuse.S_IFDIR,
			Name: name,
			Ino:  ino.StableAttr().Ino,
		})
	}

	return fs.NewListDirStream(songs), 0
}

func (album *subsonicSong) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	chil := album.GetChild(name)

	if chil != nil {
		return chil, 0
	}
	return nil, syscall.ENOENT
}

func (sr *subsonicFS) OnAdd(ctx context.Context) {
	// Construct the filesystem tree: index all of the artists and their albums

	p := &sr.Inode

	log.Println("Discovering artists...")
	artists, err := SubsonicClient.GetArtists(nil)
	if err != nil {
		log.Fatal("Fatal error! Could not get artists")
		return
	}

	for _, idx := range artists.Index {
		for _, artist := range idx.Artist {
			artistInode := p.NewPersistentInode(
				ctx,
				&subsonicArtist{
					clientObj: artist,
				},
				fs.StableAttr{Mode: fuse.S_IFDIR, Ino: hash(artist.ID)})
			p.AddChild(artist.Name, artistInode, true)
		}
	}
	log.Println("Artists successfully indexed!")
}

func main() {
	hostname := flag.String("hostname", "http://127.0.0.1:4533", "Hostname/IP Address of the Subsonic Server")
	username := flag.String("username", "user", "Username for the account")
	password := flag.String("password", "user", "Password for the account")
	mountDir := flag.String("mountDir", "/tmp/x", "Location to mount SubsonicFS")
	passwordAuth := flag.Bool("passwordAuth", false, "Whether or not to use plain-text password authentication (Default is off as it is insecure)")

	flag.Parse()

	SubsonicClient = subsonic.Client{
		Client:              &http.Client{},
		BaseUrl:             *hostname,
		User:                *username,
		ClientName:          "SubsonicFS",
		PasswordAuth:        *passwordAuth,
		RequestedAPIVersion: "1.16.1",
	}

	err := SubsonicClient.Authenticate(*password)
	if err != nil {
		log.Fatalf("Authentication failed! Check your username and password\n%s", err)
		return
	}

	root := &subsonicFS{}

	os.Mkdir(*mountDir, 0755)

	log.Printf("Logged in as: %s", SubsonicClient.User)

	log.Printf("Mounting at %s...", *mountDir)
	server, err := fs.Mount(*mountDir, root, &fs.Options{
		MountOptions: fuse.MountOptions{Debug: false},
	})

	if err != nil {
		log.Fatal(err)
	}

	log.Println("Serving...")
	server.Wait()

	fmt.Println("Quitting!")
}
