.PHONY: icons build run clean test install package

icons:
	go run ./tools/icon_gen
	cd cmd && go-winres make
	cd cmd/installer && go-winres make

build: icons
	go build -ldflags="-s -w" -o bin/anyconnect-split.exe ./cmd/
	powershell -NoProfile -Command "New-Item -ItemType Directory -Force -Path 'bin\\ui-assets' | Out-Null; Copy-Item -Force 'internal\\ui\\assets\\desktop-login-bg.png' 'bin\\ui-assets\\desktop-login-bg.png'; Copy-Item -Force 'internal\\ui\\assets\\desktop-dashboard-bg.png' 'bin\\ui-assets\\desktop-dashboard-bg.png'; Copy-Item -Force 'internal\\ui\\assets\\wechat-contact-qr.png' 'bin\\ui-assets\\wechat-contact-qr.png'"

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
