# tags
Use taglib C bindings in Go

## Usage
### Linux
#### Build Taglib
```sh
git clone --recurse-submodules https://github.com/raffleberry/tags.git

# ubuntu
sudo apt install gcc g++ cmake make libutfcpp-dev zlib1g-dev

# build & install(/usr/local/) taglib from source
./bake taglib
```
#### Install
```sh
go get github.com/raffleberry/tags
```

### Windows (Msys mingw64)
#### Install the prebuilts (2.0.1 up-to-date)
```sh
pacman -S mingw-w64-clang-x86_64-taglib mingw-w64-x86_64-gcc
```
Please refer to [Taglib Documentation](https://github.com/taglib/taglib/blob/master/INSTALL.md#windows) for building from source

#### Install
```sh
go get github.com/raffleberry/tags@windows

```


Windows doesn't support static linking(I may be wrong). Be sure to include the appropriate `.dll` files along with your **go binary** (see `bake build`)

### MacOs
Please refer to [Taglib Documentation](https://github.com/taglib/taglib/blob/master/INSTALL.md#windows) for building from source

& modify the cgo directive to find the static archives 


## Debugging
```sh
go clean -cache
```
clean before rebuilds

## Docs
[https://pkg.go.dev/github.com/raffleberry/tags](https://pkg.go.dev/github.com/raffleberry/tags)
