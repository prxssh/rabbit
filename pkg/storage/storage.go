package storage

import (
	"crypto/sha1"
	"os"
)

type BlockWrite struct {
	Offset int64
	Data   []byte
}

type Disk struct {
	f   *os.File
	wq  chan BlockWrite
	err chan error
}

func OpenSingleFile(path string, totalSize int64) (*Disk, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}

	if err := f.Truncate(totalSize); err != nil {
		_ = f.Close()
		return nil, err
	}

	d := &Disk{
		f:   f,
		wq:  make(chan BlockWrite, 1024),
		err: make(chan error, 1),
	}
	go d.loop()

	return d, nil
}

func (d *Disk) loop() {
	for bw := range d.wq {
		if _, err := d.f.WriteAt(bw.Data, bw.Offset); err != nil {
			select {
			case d.err <- err:
			default:
			}
		}
	}
}

func (d *Disk) Submit(bw BlockWrite) {
	d.wq <- bw
}

func (d *Disk) VerifyPiece(
	pieceIdx, pieceLen, pieceSize int,
	pieceHash [sha1.Size]byte,
) (bool, error) {
	buf := make([]byte, pieceSize)
	off := int64(pieceIdx) * int64(pieceLen)
	if _, err := d.f.ReadAt(buf, off); err != nil {
		return false, err
	}
	sum := sha1.Sum(buf)

	return sum == pieceHash, nil
}

func (d *Disk) Close() error {
	close(d.wq)

	if err := d.f.Sync(); err != nil {
		return err
	}

	return d.f.Close()
}

func (d *Disk) Err() error {
	select {
	case e := <-d.err:
		return e
	default:
		return nil
	}
}
