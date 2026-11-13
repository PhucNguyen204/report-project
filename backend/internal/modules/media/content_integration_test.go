//go:build integration

package media

import (
	"bytes"
	"context"
	"dicho/internal/platform"
	"encoding/binary"
	"github.com/jackc/pgx/v5"
	"hash/crc32"
	"image"
	"image/jpeg"
	"testing"
	"time"
)

// Pending, expired, truncated, or decompression-bomb uploads must never become usable attachments.
func TestPendingExpiryAndImageDecodingLimits(t *testing.T) {
	s, db, _, user, _ := setupMedia(t)
	ctx := context.Background()
	data := picture(t)
	for _, in := range []UploadInput{{"unknown", user, "image/png", 20}, {"avatar", "", "image/png", 20}, {"recipe", platform.ID(), "image/png", 20}, {"food", platform.ID(), "image/png", 20}} {
		if _, err := s.Upload(ctx, user, in); err == nil {
			t.Fatalf("invalid target accepted %+v", in)
		}
	}
	g, err := s.Upload(ctx, user, UploadInput{"avatar", user, "image/png", int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Tx(ctx, func(tx pgx.Tx) error { return s.Validate(ctx, tx, user, "avatar", user, g.ObjectID) }); err == nil {
		t.Fatal("pending image attached")
	}
	if _, err = s.Read(ctx, user, g.ObjectID); err == nil {
		t.Fatal("pending image read")
	}
	putImage(t, g, data)
	if _, err = db.Pool.Exec(ctx, "UPDATE media.objects SET expires_at=$2 WHERE id=$1", g.ObjectID, db.Clock.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = s.Complete(ctx, user, g.ObjectID); err == nil {
		t.Fatal("expired upload finalized")
	}
	bomb := append([]byte(nil), data...)
	binary.BigEndian.PutUint32(bomb[16:20], 10000)
	binary.BigEndian.PutUint32(bomb[20:24], 10000)
	binary.BigEndian.PutUint32(bomb[29:33], crc32.ChecksumIEEE(bomb[12:29]))
	for _, b := range [][]byte{data[:len(data)-10], bomb} {
		g, err = s.Upload(ctx, user, UploadInput{"avatar", user, "image/png", int64(len(b))})
		if err != nil {
			t.Fatal(err)
		}
		putImage(t, g, b)
		if err = s.Complete(ctx, user, g.ObjectID); err == nil {
			t.Fatal("unsafe or truncated image finalized")
		}
	}
	var jpg bytes.Buffer
	if err = jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	g, err = s.Upload(ctx, user, UploadInput{"avatar", user, "image/jpeg", int64(jpg.Len())})
	if err != nil {
		t.Fatal(err)
	}
	putImage(t, g, jpg.Bytes())
	if err = s.Complete(ctx, user, g.ObjectID); err != nil {
		t.Fatalf("valid JPEG rejected %v", err)
	}
}
