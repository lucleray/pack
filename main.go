package main

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type file0 struct {
	path    string
	hash    string
	version string
	offset  int
	size    int
}

const PACKAGE_PATH = ".pack"
const CURRENT_VERSION = "~"
const SEPARATOR_INDEX_CONTENT = "\n\n"

const LOGO = `
                   ▄
               ▄███▀██▄▄
           ▄▓███▄     ▀███▄▄
       ▄▓██▀   ▀██▓▄      ▀███▄
       ████▌▄      ▀██▓▄ ▄▄████
       ██░▒▀███▌▄    ▄████▀  ▓█
       ██░░░░░▒▀██████▀ ██   ▓█
       ██░░░░░░░░░██    ██   ▓█
       ██░░░░░░░░░██    ▀▀   ▓█
       ██▄░░░░░░░░██        ▄██
        ▀▀██▓▄░░░░██    ▄▄██▀▀
            ▀▀██▓▄██▄▄███▀
                ▀▀███▀
		 

`

func get_file_hash(path string) string {
	file, err := os.Open(path)

	if err != nil {
		panic(err)
	}

	defer file.Close()

	hash := md5.New()
	_, err = io.Copy(hash, file)

	if err != nil {
		panic(err)
	}

	return hex.EncodeToString(hash.Sum(nil))
}

func scan(files *[]file0, dir string, subdir string) {
	entries, err := ioutil.ReadDir(filepath.Join(dir, subdir))

	if err != nil {
		panic(err)
	}

	for _, entry := range entries {
		// TODO: only exclude in root dir, maybe?
		if entry.Name() == PACKAGE_PATH {
			continue
		}

		if entry.IsDir() {
			scan(files, dir, filepath.Join(subdir, entry.Name()))
			continue
		}

		file_path := filepath.Join(subdir, entry.Name())
		file_hash := get_file_hash(filepath.Join(dir, subdir, entry.Name()))
		file_offset := -1
		file_size := int(entry.Size())

		*files = append(*files, file0{file_path, file_hash, CURRENT_VERSION, file_offset, file_size})
	}
}

func build_index(files []file0) string {
	index_str := ""

	for i, file := range files {

		index_str = index_str + fmt.Sprintf(
			"%s\t%s\t%s\t%d\t%d",
			file.path,
			file.hash,
			file.version,
			file.offset,
			file.size,
		)

		// we want to make sure the index entry length is not changing
		// when we add the offset values
		// we do that by padding the line with spaces
		missing_offset := 32 - len(fmt.Sprintf("%d", file.offset))
		padding_str := strings.Repeat(" ", missing_offset)
		index_str = index_str + "\t" + padding_str

		if i != len(files)-1 {
			index_str = index_str + "\n"
		}
	}

	return index_str
}

func get_version(files []file0) string {
	index_str := build_index(files)

	hash := md5.New()
	_, err := io.WriteString(hash, index_str)

	if err != nil {
		panic(err)
	}

	return hex.EncodeToString(hash.Sum(nil))
}

func search_file(files []file0, path string, hash string) (bool, string, int) {
	for _, file := range files {
		if hash == file.hash {
			return true, file.version, file.offset
		}
	}

	return false, "", -1
}

// TODO: handle case where directory is empty
func pack(files []file0, version string, dir string) {
	err := os.MkdirAll(filepath.Join(dir, PACKAGE_PATH), os.ModePerm)

	if err != nil {
		panic(err)
	}

	existing_files := inspect_all(dir, []string{version})

	output, err := os.Create(filepath.Join(dir, PACKAGE_PATH, version))

	if err != nil {
		panic(err)
	}

	defer output.Close()

	// search files and point to already packaged files when available
	for i, file := range files {
		found, version, offset := search_file(existing_files, file.path, file.hash)
		if found {
			files[i].version = version
			files[i].offset = offset
		}
	}

	// adjust offsets
	index_size := len(build_index(files)) + len(SEPARATOR_INDEX_CONTENT)
	current_offset := index_size
	for i, file := range files {
		if file.version == CURRENT_VERSION {
			files[i].offset = current_offset
			current_offset = files[i].offset + int(file.size)
		}
	}

	output.WriteString(build_index(files))

	// insert file content
	files_to_insert := []file0{}
	for _, file := range files {
		if file.version == CURRENT_VERSION {
			files_to_insert = append(files_to_insert, file)
		}
	}

	if len(files_to_insert) != 0 {
		output.WriteString(SEPARATOR_INDEX_CONTENT)
	}

	for _, file := range files_to_insert {
		if file.version != CURRENT_VERSION {
			continue
		}

		input, err := os.Open(filepath.Join(dir, file.path))

		if err != nil {
			panic(err)
		}

		defer input.Close()

		io.Copy(output, input)
	}
}

func has_version(exclude_versions []string, version string) bool {
	for _, exclude_version := range exclude_versions {
		if exclude_version == version {
			return true
		}
	}
	return false
}

func inspect(version string, dir string, exclude_versions []string) []file0 {
	files := []file0{}

	package_path := filepath.Join(dir, PACKAGE_PATH)
	file, err := os.ReadFile(filepath.Join(package_path, version))

	if err != nil {
		panic(err)
	}

	index_lines := []string{}
	new_line_byte := byte('\n')
	line_start_at := 0

	for i, c := range file {
		// detect an empty line
		if c == new_line_byte && line_start_at == i {
			break
		}

		// detect end of line or end of file
		if c == new_line_byte || i == len(file)-1 {
			index_lines = append(index_lines, string(file[line_start_at:i]))
			line_start_at = i + 1
		}
	}

	for _, index_line := range index_lines {
		if len(index_line) == 0 {
			continue
		}

		// format is:
		// <path>	<hash> <version> <offset> <size>
		file_infos_str := strings.Split(index_line, "\t")

		file_version := file_infos_str[2]
		if file_version == CURRENT_VERSION {
			file_version = version
		}

		if has_version(exclude_versions, file_version) {
			continue
		}

		file_offset, err_offset := strconv.Atoi(file_infos_str[3])
		file_size, err_size := strconv.Atoi(file_infos_str[4])

		if err_offset != nil {
			panic(err_offset)
		}
		if err_size != nil {
			panic(err_size)
		}

		file := file0{
			file_infos_str[0],
			file_infos_str[1],
			file_version,
			file_offset,
			file_size,
		}

		files = append(files, file)
	}

	return files
}

func inspect_all(dir string, exclude_versions []string) []file0 {
	package_path := filepath.Join(dir, PACKAGE_PATH)
	package_entries, err := ioutil.ReadDir(package_path)

	if err != nil {
		panic(err)
	}

	files := []file0{}

	for _, package_entry := range package_entries {
		version_files := inspect(package_entry.Name(), dir, exclude_versions)
		files = append(files, version_files...)
	}

	return files
}

func unpack_file(file file0, dir string) {
	input, err := os.Open(filepath.Join(dir, PACKAGE_PATH, file.version))

	if err != nil {
		panic(err)
	}

	defer input.Close()

	output_path := filepath.Join(dir, file.path)
	err = os.MkdirAll(filepath.Dir(output_path), os.ModePerm)

	if err != nil {
		panic(err)
	}

	output, err := os.Create(filepath.Join(dir, file.path))

	if err != nil {
		panic(err)
	}

	defer output.Close()

	input.Seek(int64(file.offset), 0)
	io.CopyN(output, input, int64(file.size))
}

func reset_dir(dir string) {
	entries, err := ioutil.ReadDir(filepath.Join(dir))

	if err != nil {
		panic(err)
	}

	for _, entry := range entries {
		if entry.Name() == PACKAGE_PATH {
			continue
		}

		os.RemoveAll(filepath.Join(dir, entry.Name()))
	}
}

func unpack(version string, dir string) {
	files := inspect(version, dir, []string{})

	reset_dir(dir)

	for _, file := range files {
		unpack_file(file, dir)
	}
}

func main() {
	action := "pack"
	dir := ""
	unpack_version := ""

	os_args := os.Args[1:]
	for _, arg := range os_args {
		if strings.HasPrefix(arg, "--dir=") {
			dir = arg[len("--dir="):]
		}

		if strings.HasPrefix(arg, "--unpack=") {
			action = "unpack"
			unpack_version = arg[len("--unpack="):]
		}

		if unpack_version == "" && strings.HasPrefix(arg, "-u=") {
			action = "unpack"
			unpack_version = arg[len("-u="):]
		}

		if arg == "--inspect" || arg == "-i" {
			action = "inspect"
		}

		if arg == "--help" || arg == "-h" {
			action = "help"
		}
	}

	if action == "pack" {
		files := []file0{}
		scan(&files, dir, "")
		pack_version := get_version(files)
		pack(files, pack_version, dir)
		fmt.Println("📦", pack_version)
	}

	if action == "unpack" {
		// TODO: we could first "pack" in the background, so no files are lost
		unpack(unpack_version, dir)
	}

	if action == "help" {
		fmt.Print(LOGO)
	}

	if action == "inspect" {
		files := inspect_all(dir, []string{})
		for _, file := range files {
			fmt.Printf(
				"%-20s\t%s\t%s\t%-5d\t%-5d\n",
				file.path,
				file.hash,
				file.version,
				file.offset,
				file.size,
			)
		}
	}
}
