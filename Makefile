# minidb Makefile（M1：内存版）
#
# go 工具链不在默认 PATH 里时这样调：
#   PATH="$HOME/go/bin:$PATH" make check

GO ?= go

.PHONY: check test vet fmt bench examples clean

# check：vet + 全部测试。
check: vet test

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

# fmt：检查 gofmt。
fmt:
	@test -z "$$($(GO)fmt -l .)" || ($(GO)fmt -l . && exit 1)

# bench：M1 不测性能。目标先占位，M5 接入基准测试。
bench:
	@echo "M1 不测性能：基准测试在 M5 接入（插入/点查/范围扫描）。"
	@echo "如需看执行速度，可先用 time minidb run examples/basic.sql 粗测。"

# examples：跑示例 SQL。
examples:
	$(GO) run ./cmd/minidb run examples/basic.sql

clean:
	$(GO) clean ./...
