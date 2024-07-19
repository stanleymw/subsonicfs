package main

import (
	// "archive/zip"
	"context"
	"flag"
	"fmt"
	// "io"
	"log"
	"net/http"
	"os"
	// "sync"
	"syscall"

	"github.com/dweymouth/go-subsonic/subsonic"
	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

// zipFile is a file read from a zip archive.
//
// // We decompress the file on demand in Open
// var _ = (fs.NodeOpener)((*zipFile)(nil))
//
// // Getattr sets the minimum, which is the size. A more full-featured
// // FS would also set timestamps and permissions.
// var _ = (fs.NodeGetattrer)((*zipFile)(nil))
//
//	func (zf *zipFile) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
//		out.Size = zf.file.UncompressedSize64
//		return 0
//	}
//
// // Open lazily unpacks zip data
//
//	func (zf *zipFile) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
//		zf.mu.Lock()
//		defer zf.mu.Unlock()
//		if zf.data == nil {
//			rc, err := zf.file.Open()
//			if err != nil {
//				return nil, 0, syscall.EIO
//			}
//			content, err := io.ReadAll(rc)
//			if err != nil {
//				return nil, 0, syscall.EIO
//			}
//
//			zf.data = content
//		}
//
//		// We don't return a filehandle since we don't really need
//		// one.  The file content is immutable, so hint the kernel to
//		// cache the data.
//		return nil, fuse.FOPEN_KEEP_CACHE, fs.OK
//	}
//
// // Read simply returns the data that was already unpacked in the Open call
//
//	func (zf *zipFile) Read(ctx context.Context, f fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
//		end := int(off) + len(dest)
//		if end > len(zf.data) {
//			end = len(zf.data)
//		}
//		return fuse.ReadResultData(zf.data[off:end]), fs.OK
//	}
//
// zipRoot is the root of the Zip filesystem. Its only functionality
// is populating the filesystem.
type subsonicFS struct {
	fs.Inode

	subsonicClient *subsonic.Client
}

type subsonicAlbum struct {
	fs.Inode

	album          *subsonic.Child
	subsonicClient *subsonic.Client
}

type subsonicSong struct {
	fs.Inode

	subsonicClient *subsonic.Client
}

// The root populates the tree in its OnAdd method
var _ = (fs.NodeOnAdder)((*subsonicFS)(nil))

func (alb *subsonicAlbum) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	return fs.NewLoopbackDirStream("/")
}

// func (alb *subsonicAlbum) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
// 	out.Ctime = uint64(alb.album.Created.UnixMicro())
// 	return 0
// }

func (alb *subsonicAlbum) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	log.Println("NODE LOOKED UP!!!")
	out.Ctime = uint64(alb.album.Created.UnixMicro())
	out.Atime = uint64(alb.album.Created.UnixMicro())
	out.Mtime = uint64(alb.album.Created.UnixMicro())
	return &alb.Inode, 0
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
		// for _, component := range strings.Split(dir, "/") {
		// 	if len(component) == 0 {
		// 		continue
		// 	}
		// 	ch := p.GetChild(component)
		// 	if ch == nil {
		// 		ch = p.NewPersistentInode(ctx, &fs.Inode{},
		// 			fs.StableAttr{Mode: fuse.S_IFDIR})
		// 		p.AddChild(component, ch, true)
		// 	}
		//
		// 	p = ch
		// }
		//
		// if f.FileInfo().IsDir() {
		// 	continue
		// }

		// ch := p.NewPersistentInode(ctx, &fs.Inode{}, fs.StableAttr{Mode: fuse.S_IFDIR})
		// p.AddChild(f.Title, ch, true)
		log.Println(f)

		ch := p.NewPersistentInode(ctx,
			&subsonicAlbum{album: f,
				subsonicClient: sr.subsonicClient},
			fs.StableAttr{Mode: fuse.S_IFDIR})
		p.AddChild(f.Title, ch, true)
	}
}

// ExampleZipFS shows an in-memory, static file system
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

	// fmt.Println("zip file mounted")
	// fmt.Printf("to unmount: fusermount -u %s\n", mnt)

	fmt.Println("waiting...")
	server.Wait()

	fmt.Println("DONE!")
}
