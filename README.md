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
release directory, create a deployment-specific environment file, and run as
root:

```sh
sudo ./scripts/install.sh /path/to/my_assistant-linux-arm64
```

The release archive contains `install.sh`, so the equivalent command from the
extracted directory is:

```sh
sudo ./install.sh .
```

The installer registers and starts the systemd service. Do not use
`.env.example` as production configuration; copy it to the protected path and
replace placeholder values first. The release build compiles and embeds the
frontend assets in the backend binary. Device discovery and Raspberry Pi reboot
recovery testing remain deployment tasks.

---
## TODOs

---
## History

---
## Info

- Author : Louie Yang
- Base HW : RPI4
- OS : Ubuntu 22.04
