.PHONY: icons build run clean test install package

icons:
	go run ./tools/icon_gen
	cd cmd && go-winres make
	cd cmd/installer && go-winres make

build: icons
	go build -ldflags="-s -w" -o bin/anyconnect-split.exe ./cmd/

run:
	go run ./cmd/

clean:
	rm -rf bin/

test:
	go test ./internal/... -v

install: build
	powershell -Command "$$ws = New-Object -ComObject WScript.Shell; $$sc = $$ws.CreateShortcut([Environment]::GetFolderPath('Desktop') + '\\Split Tunnel.lnk'); $$sc.TargetPath = '$(CURDIR)\\bin\\anyconnect-split.exe'; $$sc.WorkingDirectory = '$(CURDIR)\\bin'; $$sc.IconLocation = '$(CURDIR)\\bin\\app.ico'; $$sc.Description = 'AnyConnect Split Tunnel'; $$sc.Save()"

package:
	powershell -ExecutionPolicy Bypass -File tools/package.ps1
