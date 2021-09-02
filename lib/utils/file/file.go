/*
Copyright 2021 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package file

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"

	"github.com/gravitational/trace"
)

type File struct {
	Path string
	Data []byte
	Mode os.FileMode
}

func EnsureFiles(files []*File) error {
	for _, file := range files {
		if _, err := EnsureFile(file); err != nil {
			return trace.Wrap(err)
		}
	}
	return nil
}

func EnsureFile(f *File) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0755); err != nil {
		return false, err
	}
	fs, err := os.Stat(f.Path)
	if os.IsNotExist(err) {
		if err = ioutil.WriteFile(f.Path, f.Data, f.Mode); err != nil {
			return false, err
		}
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if fs.Size() != int64(len(f.Data)) {
		if err = ioutil.WriteFile(f.Path, f.Data, f.Mode); err != nil {
			return false, err
		}
		return true, nil
	}
	oldData, err := ioutil.ReadFile(f.Path)
	if err != nil {
		return false, err
	}
	if !reflect.DeepEqual(oldData, f.Data) {
		if err = ioutil.WriteFile(f.Path, f.Data, f.Mode); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}
