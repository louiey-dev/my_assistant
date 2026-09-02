# my_assistant

This is Web Home monitoring system app which is similar with home assistant
---
## Specification

- Monitoring sensors, cameras and display these info at web page
- add sensor devices via mqtt
- add cameras via mqtt and display via RTSP and so on
- UI which look for external devices like a sensor and camera
- UI which displays camera
- UI which read sensor values and display via graph
- UI which write command to device
- connect to via ip address
- Add setup requirements for the Raspberry Pi, installation commands, configuration/environment variables, and examples

---
## Build and deploy

Build the backend release from WSL Ubuntu 22.04:

```sh
./scripts/build-release.sh
```

The command cross-compiles a `linux/arm64` binary and creates
`dist/my_assistant-linux-arm64.tar.gz`. On the Raspberry Pi, copy the extracted
release directory and a deployment-specific environment file, then run as
root:

```sh
sudo ./scripts/install.sh /path/to/my_assistant-linux-arm64
```

The installer registers and starts the systemd service. Do not use
`.env.example` as production configuration; copy it to the protected path and
replace placeholder values first. Frontend assets and Raspberry Pi reboot
recovery testing remain pending until the frontend and production test device
are available.

---
## TODOs

---
## History

---
## Info

- Author : Louie Yang
- Base HW : RPI4
- OS : Ubuntu 22.04
