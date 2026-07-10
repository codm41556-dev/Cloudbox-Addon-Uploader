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
	"database/sql"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	_ "github.com/go-sql-driver/mysql"
)

var (
	handle   *sql.DB
	s3client *s3.Client
)

// Init connects to the database and configures S3 storage. s3endpoint is
// optional: leave it empty to use normal AWS S3 (the original behavior).
// Set it (e.g. "http://127.0.0.1:9000") to point at a local S3-compatible
// server such as MinIO for local development/testing - this is the only
// thing that changes vs. upstream, purely to make local testing possible
// without real AWS credentials or buckets.
func Init(username string, password string, protocol string, address string, database string, s3endpoint string) error {
	db, err := sql.Open("mysql", fmt.Sprintf("%s:%s@%s(%s)/%s?parseTime=true", username, password, protocol, address, database))
	if err != nil {
		return err
	}

	handle = db

	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return err
	}

	if s3endpoint != "" {
		s3client = s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(s3endpoint)
			o.UsePathStyle = true
		})
	} else {
		s3client = s3.NewFromConfig(cfg)
	}

	return nil
}
