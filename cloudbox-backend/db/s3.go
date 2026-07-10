/*
   cloudbox - the toybox server emulator
   Copyright (C) 2024-2025  patapancakes <patapancakes@pagefault.games>

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU Affero General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU Affero General Public License for more details.

   You should have received a copy of the GNU Affero General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

package db

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func GetContentFile(ctx context.Context, id int) (*s3.GetObjectOutput, error) {
	o, err := s3client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String("flatgrass-toybox-content"),
		Key:    aws.String(strconv.Itoa(id)),
	})
	if err != nil {
		return nil, err
	}

	return o, nil
}

// PutContentFile stores raw content bytes (models/materials/lua/etc, one
// physical file) under the given file id, matching the key scheme
// GetContentFile already reads from. Content is stored uncompressed, same
// as existing recovered Toybox content (see api/content/getzip.go, which
// wraps it in a zip container at request time only, on the fly).
func PutContentFile(ctx context.Context, id int, data io.Reader) error {
	_, err := s3client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String("flatgrass-toybox-content"),
		Key:    aws.String(strconv.Itoa(id)),
		Body:   data,
	})
	if err != nil {
		return err
	}

	return nil
}

// DeleteContentFile removes raw content bytes for the given file id from S3.
func DeleteContentFile(ctx context.Context, id int) error {
	_, err := s3client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String("flatgrass-toybox-content"),
		Key:    aws.String(strconv.Itoa(id)),
	})
	if err != nil {
		return err
	}

	return nil
}

// PutThumbnail stores a 128x128 PNG thumbnail for the given package id under
// the same key scheme the public CDN (img.cl0udb0x.com) and the new
// /content/thumb endpoint both read from. The caller is responsible for
// passing already-encoded PNG bytes (see api/addons/upload.go, which decodes
// JPG/PNG input and re-encodes to PNG before calling this).
func PutThumbnail(ctx context.Context, id int, data io.Reader) error {
	_, err := s3client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String("flatgrass-toybox-image"),
		Key:    aws.String(fmt.Sprintf("%d_thumb_128.png", id)),
		ACL:    types.ObjectCannedACLPublicRead,
		Body:   data,
	})
	if err != nil {
		return err
	}

	return nil
}

// GetThumbnail fetches the stored 128x128 PNG thumbnail for a package id, so
// the backend can serve it itself (GET /content/thumb?id=) instead of relying
// on the external img.cl0udb0x.com CDN existing - important for self-hosted /
// locally-tested setups where there is no CDN in front of the S3 bucket.
func GetThumbnail(ctx context.Context, id int) (*s3.GetObjectOutput, error) {
	o, err := s3client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String("flatgrass-toybox-image"),
		Key:    aws.String(fmt.Sprintf("%d_thumb_128.png", id)),
	})
	if err != nil {
		return nil, err
	}

	return o, nil
}

// DeleteThumbnail removes the stored thumbnail for a package id. Called when a
// self-uploaded addon is deleted so its (possibly user-uploaded) icon doesn't
// linger in S3 after the package row is gone. Best-effort: a missing or failed
// delete here must not block removing the database record.
func DeleteThumbnail(ctx context.Context, id int) error {
	_, err := s3client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String("flatgrass-toybox-image"),
		Key:    aws.String(fmt.Sprintf("%d_thumb_128.png", id)),
	})
	if err != nil {
		return err
	}

	return nil
}
