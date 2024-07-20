package main

import (
	//"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"path"

	"log"
	"net/http"
	"os"

	// "sync"
	"syscall"

	"github.com/dweymouth/go-subsonic/subsonic"
	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

type subsonicFS struct {
	fs.Inode

	subsonicClient *subsonic.Client
}

type subsonicAlbum struct {
	fs.Inode

	album          *subsonic.Child
	subsonicClient *subsonic.Client
	pathToSong     map[string]*subsonic.Child
}

type subsonicSong struct {
	fs.Inode

	subsonicClient *subsonic.Client
	songObj        *subsonic.Child
	streamer       *io.Reader
	dled           []byte
}

// The root populates the tree in its OnAdd method
var _ = (fs.NodeOnAdder)((*subsonicFS)(nil))

func (songf *subsonicSong) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	log.Println("JUST TRIED TO OPEN!")
	stmr, err := songf.subsonicClient.Stream(songf.songObj.ID, nil)
	if err != nil {
		return nil, 0, syscall.ENOENT
	}

	songf.streamer = &stmr
	return songf, 0, 0
}

// TODO: create some reader/writerat new class -> so whenever an offset greater than the current cached data is read: we need to update this new class. essentially CACHE ALL THE READS TO THE CURRENT OFFSET!
// idea: should not read the entire stream at once
// instead, we need to maintain the current highest offset. If newOffset > internal high offset then further reading is required. Otherwise, the previously cached data can be returned (as the new offset sohuld be within the good range - already read data)
func (songf *subsonicSong) Read(ctx context.Context, f fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	bufStreamer := *(songf.streamer)
	if songf.dled == nil {
		dl, err := io.ReadAll(bufStreamer)
		log.Println("EXP READALL")
		songf.dled = dl
		if err != nil {
			return fuse.ReadResultData(dest), syscall.ENOENT
		}
	}
	nreader := bytes.NewReader(songf.dled)
	nreader.ReadAt(dest, off)

	//log.Printf("READING dest->%s | off->%s \n", dest, off)
	return fuse.ReadResultData(dest), 0
}

func (songf *subsonicSong) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	sobj := songf.songObj
	out.Ctime = uint64(sobj.Created.Unix())
	out.Atime = uint64(sobj.Created.Unix())
	out.Mtime = uint64(sobj.Created.Unix())
	out.Mode = syscall.S_IFREG
	out.Size = uint64(sobj.Size)

	return 0
}

func (alb *subsonicAlbum) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	album, err := alb.subsonicClient.GetAlbum(alb.album.ID)
	if err != nil {
		return nil, syscall.ENOENT
	}

	songs := []fuse.DirEntry{}
	for _, song := range album.Song {
		fmt.Printf("SONG ID: %s\n", song.ID)
		//songs.append(song.Title)
		songs = append(songs, fuse.DirEntry{
			Mode: fuse.S_IFREG,
			Name: path.Base(song.Path),
		})
		alb.pathToSong[path.Base(song.Path)] = song
	}

	return fs.NewListDirStream(songs), 0
}

// func (alb *subsonicAlbum) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
// 	out.Ctime = uint64(alb.album.Created.UnixMicro())
// 	return 0
// }

func (alb *subsonicAlbum) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	val, ok := alb.pathToSong[name]
	if ok {

	} else {
		return nil, syscall.ENOENT
	}

	log.Println("NODE FOUND!")

	ssong := subsonicSong{subsonicClient: alb.subsonicClient, songObj: val}
	out.Ctime = uint64(val.Created.Unix())
	out.Atime = uint64(val.Created.Unix())
	out.Mtime = uint64(val.Created.Unix())
	out.Mode = syscall.S_IFREG
	out.Size = uint64(val.Size)

	log.Println("lookup FINISH!")
	return alb.NewInode(ctx, &ssong, fs.StableAttr{Mode: syscall.S_IFREG}), 0
}

func (sr *subsonicFS) OnAdd(ctx context.Context) {
	// OnAdd is called once we are attached to an Inode. We can
	//		// then construct a tree.  We construct the entire tree, and
	//		// we don't want parts of the tree to disappear when the
	//		// kernel is short on memory, so we use persistent inodes.
	albums, err := sr.subsonicClient.GetAlbumList("newest", nil)
	if err != nil {
		return
	}
	for _, f := range albums {
		// dir, base := filepath.Split(f.Name)

		p := &sr.Inode

		// ch := p.NewPersistentInode(ctx, &fs.Inode{}, fs.StableAttr{Mode: fuse.S_IFDIR})
		// p.AddChild(f.Title, ch, true)
		log.Println(f)

		ch := p.NewPersistentInode(ctx,
			&subsonicAlbum{album: f,
				subsonicClient: sr.subsonicClient,
				pathToSong:     map[string]*subsonic.Child{}},
			fs.StableAttr{Mode: fuse.S_IFDIR})
		p.AddChild(f.Title, ch, true)
	}
}

func main() {
	flag.Parse()
	// if len(flag.Args()) != 1 {
	// 	log.Fatal("usage: subsonicfs HOST USERNAME PASSWORD")
	// }
	// zfile, err := zip.OpenReader(flag.Arg(0))
	// if err != nil {
	// 	log.Fatal(err)
	// }
	username := "user"
	password := "user"
	hostname := "http://127.0.0.1:4533"

	subsonicClient := subsonic.Client{
		Client:       &http.Client{},
		BaseUrl:      hostname,
		User:         username,
		ClientName:   "SubsonicFS",
		PasswordAuth: false,
	}

	subsonicClient.Authenticate(password)

	folders, _ := subsonicClient.GetMusicFolders()
	fmt.Println(folders)
	// indexes, err := subsonicClient.GetIndexes(nil)
	for i, v := range folders {
		fmt.Printf("GOT ONE: %s %s\n", i, v)
		indexes, _ := subsonicClient.GetIndexes(map[string]string{"musicFolderId": v.ID})
		// dirs, _ := subsonicClient.GetMusicDirectory(v.ID)

		fmt.Printf("idxs: %s\n", indexes)
		for _, b := range indexes.Index {
			fmt.Println(b)
		}
	}

	// if err != nil {
	// 	log.Fatal(err)
	// }
	root := &subsonicFS{subsonicClient: &subsonicClient}

	mnt := "/tmp/x"
	os.Mkdir(mnt, 0755)

	fmt.Println("mounting...")
	server, err := fs.Mount(mnt, root, &fs.Options{
		MountOptions: fuse.MountOptions{Debug: true},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("waiting...")
	server.Wait()

	fmt.Println("DONE!")
}
