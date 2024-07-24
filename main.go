package main

import (
	"context"
	"flag"
	"fmt"
	"hash/fnv"
	"log"
	"net/http"
	"os"
	"path"
	"syscall"

	"stanleymw/subsonicfs/readbuf"

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

type subsonicAlbum struct {
	fs.Inode

	clientObj *subsonic.AlbumID3
}

type subsonicSong struct {
	fs.Inode

	clientObj *subsonic.Child
	streamer  *readbuf.ReaderBuf
}

// The root populates the tree in its OnAdd method
var _ = (fs.NodeOnAdder)((*subsonicFS)(nil))

func (song *subsonicSong) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	if song.streamer != nil {
		return &song, fuse.FOPEN_DIRECT_IO, 0
	}

	stmr, err := SubsonicClient.Stream(song.clientObj.ID, nil)
	if err != nil {
		return nil, 0, syscall.ENOENT
	}

	song.streamer = readbuf.NewReaderBuf(&stmr, song.clientObj.Size)
	return &song, fuse.FOPEN_DIRECT_IO, 0
}

func (song *subsonicSong) Read(ctx context.Context, f fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	readStart := off
	readEnd := min(off+int64(len(dest)), int64(len(*song.streamer.InternalCache)))

	song.streamer.EnsureCached(readStart, readEnd)

	return fuse.ReadResultData((*song.streamer.InternalCache)[readStart:readEnd]), 0
}
func (song *subsonicSong) Flush(ctx context.Context, f fs.FileHandle) syscall.Errno {
	song.streamer = nil
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

	// log.Printf("[Readdir] readdir called by album: %s\n", album)
	songs := []fuse.DirEntry{}
	for name, ino := range album.Children() {
		// fmt.Printf("SONG ID: %s\n", song.ID)
		//songs.append(song.Title)
		songs = append(songs, fuse.DirEntry{
			Mode: fuse.S_IFREG,
			Name: name,
			Ino:  ino.StableAttr().Ino,
		})
		// album.children[path.Base(song.Path)] = hash(song.ID)
		// alb.pathToSong[path.Base(song.Path)] = song
	}

	return fs.NewListDirStream(songs), 0
}

// func (alb *subsonicAlbum) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
// 	out.Ctime = uint64(alb.album.Created.UnixMicro())
// 	return 0
// }

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
	log.Println("Discovering albums...")
	for _, idx := range artists.Index {
		for _, artist := range idx.Artist {
			artistInode := p.NewPersistentInode(
				ctx,
				&fs.Inode{},
				fs.StableAttr{Mode: fuse.S_IFDIR, Ino: hash(artist.ID)})
			p.AddChild(artist.Name, artistInode, true)

			// log.Printf("just got artist %s | albums: %s\n", artist.Name, artist.Album)

			// subsonic doesnt return album data within the artist call
			art2, _ := SubsonicClient.GetArtist(artist.ID)
			for _, album := range art2.Album {
				// dir, base := filepath.Split(f.Name)
				// ch := p.NewPersistentInode(ctx, &fs.Inode{}, fs.StableAttr{Mode: fuse.S_IFDIR})
				// p.AddChild(f.Title, ch, true)

				albumInode := artistInode.NewPersistentInode(
					ctx,
					&subsonicAlbum{
						clientObj: album,
					},
					fs.StableAttr{Mode: fuse.S_IFDIR, Ino: hash(album.ID)})
				artistInode.AddChild(fmt.Sprint(album.Name, " (", album.Year, ")"), albumInode, true)
			}
		}
	}
	log.Println("Artists and albums successfully indexed!")
}

func main() {
	hostname := flag.String("hostname", "http://127.0.0.1:4533", "Hostname/IP Address of the Subsonic Server")
	username := flag.String("username", "user", "Username for the account")
	password := flag.String("password", "user", "Password for the account")
	mountDir := flag.String("mountDir", "/tmp/SubsonicFS", "Location to mount SubsonicFS")
	passwordAuth := flag.Bool("passwordAuth", false, "Whether or not to use plain-text password authentication (Default is off as it is insecure)")

	flag.Parse()

	SubsonicClient = subsonic.Client{
		Client:       &http.Client{},
		BaseUrl:      *hostname,
		User:         *username,
		ClientName:   "SubsonicFS",
		PasswordAuth: *passwordAuth,
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
