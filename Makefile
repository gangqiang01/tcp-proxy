TARGETBIN=TCP-PROXY
.PHONY:	all ${TARGETBIN} 

BUILD_ROOT=$(PWD)
all: ${TARGETBIN}

${TARGETBIN}:
	@gofmt -l -w ${BUILD_ROOT}/
	@export GO111MODULE=on && \
	export GOPROXY=https://goproxy.cn && \
	go build -ldflags "-w -s" -o $@ ./main.go
	@chmod 777 $@

.PHONY: clean  install
clean:
	@rm -rf${TARGETBIN} *.log *.db *.zip
	@echo "[clean Done]"
