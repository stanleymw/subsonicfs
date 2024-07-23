package main

import (
	//"bufio"
	//"bytes"
	"context"
	"flag"
	"fmt"
	"slices"

	//"io"
	"hash/fnv"
	"path"
	"stanleymw/subsonicfs/readbuf"
	// "strings"

	"log"
	"net/http"
	"os"

	// "sync"
	"syscall"

	"github.com/dweymouth/go-subsonic/subsonic"
	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

var hasher = fnv.New64()

func hash(s string) uint64 {
	hasher.Reset()
	hasher.Write([]byte(s))
	return hasher.Sum64()
}

type subsonicFS struct {
	fs.Inode

	subsonicClient *subsonic.Client
}

// type subsonicAlbum struct {
// 	fs.Inode
//
// 	album          *subsonic.Child
// 	subsonicClient *subsonic.Client
// 	pathToSong     map[string]*subsonic.Child
// }

type subsonicObj struct {
	fs.Inode

	subsonicClient *subsonic.Client
	clientObj      *subsonic.Child

	streamer *readbuf.ReaderBuf
	children map[string]*fs.Inode
	// streamer *io.Reader
	// dled     []byte
}

// The root populates the tree in its OnAdd method
var _ = (fs.NodeOnAdder)((*subsonicFS)(nil))

// var inode_object_map map[uint64]*subsonicObj

func (song *subsonicObj) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	log.Printf("open(%s)@%p WITH streamer=%p called\n", song, song, song.streamer)
	if song.streamer != nil {
		log.Println("reurning a used streamer")
		return &song, fuse.FOPEN_DIRECT_IO, 0
	}

	log.Println("!!CREATING A NEW STREAMER!!!")

	stmr, err := song.subsonicClient.Stream(song.clientObj.ID, nil)
	if err != nil {
		return nil, 0, syscall.ENOENT
	}

	song.streamer = readbuf.NewReaderBuf(&stmr, song.clientObj.Size)
	return &song, fuse.FOPEN_DIRECT_IO, 0
}

// TODO: create some reader/writerat new class -> so whenever an offset greater than the current cached data is read: we need to update this new class. essentially CACHE ALL THE READS TO THE CURRENT OFFSET!
// idea: should not read the entire stream at once
// instead, we need to maintain the current highest offset. If newOffset > internal high offset then further reading is required. Otherwise, the previously cached data can be returned (as the new offset sohuld be within the good range - already read data)
func (song *subsonicObj) Read(ctx context.Context, f fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	log.Printf("READ() song %s to dest=%s at off=%s", song, dest, off)
	// bufStreamer := *(songf.streamer)
	// if songf.dled == nil {
	// 	dl, err := io.ReadAll(bufStreamer)
	//
	// 	log.Println("EXP READALL")
	//
	// 	songf.dled = dl
	// 	if err != nil {
	// 		return fuse.ReadResultData(dest), syscall.ENOENT
	// 	}
	// }
	// nreader := bytes.NewReader(songf.dled)
	// nreader.ReadAt(dest, off)

	bufReader := *(song.streamer)
	// bufReader.ReadAt(&dest, off)

	log.Printf("[read] song addy: %p | streamer addy: %p\n", song, bufReader)

	readStart := off
	readEnd := min(off+int64(len(dest)), int64(len(*bufReader.InternalCache)))

	log.Printf("[read] PRE-ENSURE cache: %d\n", bufReader.ReadPosition)
	bufReader.EnsureCached(readStart, readEnd)
	log.Printf("[read] POST-ENSURE cache: %d\n", bufReader.ReadPosition)

	temp := make([]byte, readEnd-readStart)
	copy(temp, (*bufReader.InternalCache)[readStart:readEnd])

	//log.Printf("READING dest->%s | off->%s \n", dest, off)
	return fuse.ReadResultData(temp), 0
}

func (song *subsonicObj) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	sobj := song.clientObj
	out.Ctime = uint64(sobj.Created.Unix())
	out.Atime = uint64(sobj.Created.Unix())
	out.Mtime = uint64(sobj.Created.Unix())
	out.Mode = syscall.S_IFREG
	out.Size = uint64(sobj.Size)
	out.Ino = hash(sobj.ID)

	return 0
}

func (album *subsonicObj) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	log.Printf("[Readdir] readdir called by album: %s\n", album)
	if !album.clientObj.IsDir {
		return nil, syscall.ENOTDIR
	}

	albumInfo, err := album.subsonicClient.GetAlbum(album.clientObj.ID)

	if err != nil {
		return nil, syscall.ENOENT
	}

	songs := []fuse.DirEntry{}
	for _, song := range albumInfo.Song {
		fmt.Printf("SONG ID: %s\n", song.ID)
		//songs.append(song.Title)
		songs = append(songs, fuse.DirEntry{
			Mode: fuse.S_IFREG,
			Name: path.Base(song.Path),
			Ino:  hash(song.ID),
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

func (album *subsonicObj) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	val, ok := album.children[name]
	if ok {
		log.Println("[lookup]!!NEW returning already allocated inode")
		return val, 0
	}

	if !album.clientObj.IsDir {
		return nil, syscall.ENOTDIR
	}

	albumInfo, err := album.subsonicClient.GetAlbum(album.clientObj.ID)
	// val, ok := alb.pathToSong[name]
	if err != nil {
		return nil, syscall.ENOENT
	}

	song_idx := slices.IndexFunc(albumInfo.Song, func(s *subsonic.Child) bool { return path.Base(s.Path) == name })
	// for _, song := range albumInfo.Song {
	// 	if path.Base(song.Path) == name {
	// 		found_song = subsonicObj{Inode: hash(song.ID), subsonicClient: album.subsonicClient, clientObj: song}
	// 	}
	// }

	// if found_song {
	// 	return nil, syscall.ENOENT
	// }
	if song_idx == -1 {
		return nil, syscall.ENOENT
	}
	found_song := albumInfo.Song[song_idx]

	ssong := &subsonicObj{subsonicClient: album.subsonicClient, clientObj: found_song}
	log.Println("[lookup] !!!!!! NEW SUBSONIC SONG OBJECT ALLOCATED")
	// log.Println("NODE FOUND!")

	// ssong := subsonicSong{subsonicClient: alb.subsonicClient, songObj: val}
	out.Ctime = uint64(found_song.Created.Unix())
	out.Atime = uint64(found_song.Created.Unix())
	out.Mtime = uint64(found_song.Created.Unix())
	out.Mode = syscall.S_IFREG
	out.Size = uint64(found_song.Size)
	out.Ino = hash(found_song.ID)

	// log.Println("lookup FINISH!")
	in := album.NewPersistentInode(ctx, ssong, fs.StableAttr{Mode: syscall.S_IFREG, Ino: hash(found_song.ID)})

	album.children[name] = in
	album.AddChild(name, in, false)
	return in, 0
}

func (sr *subsonicFS) OnAdd(ctx context.Context) { // OnAdd is called once we are attached to an Inode. We can
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
			&subsonicObj{clientObj: f,
				subsonicClient: sr.subsonicClient, children: map[string]*fs.Inode{}},
			fs.StableAttr{Mode: fuse.S_IFDIR, Ino: hash(f.ID)})
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
	//
	username := "user"
	password := "user"
	hostname := "http://localhost:4533"

	subsonicClient := subsonic.Client{
		Client:       &http.Client{},
		BaseUrl:      hostname,
		User:         username,
		ClientName:   "SubsonicFS",
		PasswordAuth: false,
	}

	// r := strings.NewReader("Hello, Reader!")
	// rb := readbuf.NewReaderBuf(r, 100)
	//
	// var arr [50]byte
	// rb.ReadAt(arr[:], 3)
	// log.Println(arr)

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
		MountOptions: fuse.MountOptions{Debug: false},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("waiting...")
	server.Wait()

	fmt.Println("DONE!")
}
