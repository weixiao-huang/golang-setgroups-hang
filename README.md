# Setgroups hanging bug while using golang 1.16+

## 1. How to reproduce

### 1.1 Build docker image

build the test image
```shell
docker build --build-arg=GOPROXY=$GOPROXY --build-arg BUILD_IMAGE=golang:1.16 -t demo:1.16 .
docker build --build-arg=GOPROXY=$GOPROXY --build-arg BUILD_IMAGE=golang:1.15.15 -t demo:1.15.15 .
```

### 1.2 Running server and client in docker

Firstly run demo:1.16 and demo:1.15.15

```shell
docker run -d --rm --name=demo-1.15 demo:1.15.15 server --key-path /.launch/key --bind 0.0.0.0:2222
docker run -d --rm --name=demo-1.16 demo:1.16 server --key-path /.launch/key --bind 0.0.0.0:2222
```

Then use both container to run client in containers separately

This command will exec successfully: 
```shell
# In demo-1.15 compiled by golang:1.15.15
docker exec -it demo-1.15 su -c "client --key-path /.launch/key --server=localhost:2222" demo
```

This command will hang:
```shell
# In demo-1.16 compiled by golang:1.16
docker exec -it demo-1.16 su -c "client --key-path /.launch/key --server=localhost:2222" demo
```

and if you use `docker logs -f demo-1.16`, you will see the log below:

```text
time="2021-12-12T07:32:03Z" level=info msg="Waiting for connection"
time="2021-12-12T07:32:25Z" level=info msg="Connection established" remote="127.0.0.1:37052"
time="2021-12-12T07:32:25Z" level=info msg="--------- pty-req ---------"
time="2021-12-12T07:32:25Z" level=info msg="~~~~~~~~~~ 1 ~~~~~~~~~"
time="2021-12-12T07:32:25Z" level=info msg="[copy pty to sshChan] ~~~~~~~~~~ 1.2.1 ~~~~~~~~~"
time="2021-12-12T07:32:25Z" level=info msg="[copy sshChan to pty] ~~~~~~~~~~ 1.1.1 ~~~~~~~~~"
time="2021-12-12T07:32:25Z" level=info msg="--------- exec ---------"
time="2021-12-12T07:32:25Z" level=info msg="========= 1 ==========: &{UID:10250 GID:10250 GIDs:[27 44 10250] Path:/bin/bash Env:[HOSTNAME=4f7565a3de5f USER=demo PWD=/ HOME=/home/demo MAIL=/var/mail/demo SHELL=/bin/bash TERM=xterm SHLVL=1 LOGNAME=demo PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/games:/usr/local/games:/snap/bin _=/usr/local/bin/client] Argv:[/bin/bash] Dir:/ NumFiles:1024}"
time="2021-12-12T07:32:25Z" level=info msg="========= 2 ==========: [27 44 10250]"
```

and the code is in `server/exec.go:101` 

```go
	log.Infof("========= 2 ==========: %+v", lr.GIDs)
	if len(lr.GIDs) != 0 {
		if err := syscall.Setgroups(lr.GIDs); err != nil { // <- Server hangs here
			log.WithError(err).Warn("Setgroups")
		}
	}
```

## 2. Some tips

- If we compile these codes by using `CGO_ENABLED=1`, this problem will not be happened.
- If we uncomment the function `CopyWithContext` called in `server/pty.go:45`, `syscall.Setgroups` will not hang.
