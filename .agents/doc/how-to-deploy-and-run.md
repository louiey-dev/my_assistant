# How to deploy and run

This guide builds the application on WSL Ubuntu and runs it on the Raspberry
Pi 4 at `192.168.1.10`.

## 1. Build the ARM64 release in WSL

From the project directory:

```bash
cd /home/louiey/Work/RPI/my_assistant
source ~/.nvm/nvm.sh

./scripts/build-release.sh
```

The command builds the frontend, embeds the frontend assets in the Go binary,
cross-compiles the backend for Linux ARM64, and creates:

```text
dist/my_assistant-linux-arm64.tar.gz
```

## 2. Copy the release to the Raspberry Pi

```bash
ssh pi@192.168.1.10 'mkdir -p ~/Work'
scp dist/my_assistant-linux-arm64.tar.gz pi@192.168.1.10:~/Work/
```

Then connect to the Pi:

```bash
ssh pi@192.168.1.10
```

The commands below use `~/Work` as a persistent staging directory. You can
replace it with another directory you created on the Pi.

## 3. Extract and configure

```bash
cd ~/Work
mkdir -p my_assistant-linux-arm64
tar -xzf my_assistant-linux-arm64.tar.gz \
  -C my_assistant-linux-arm64 --strip-components=1
cd my_assistant-linux-arm64

cp my_assistant.env.example my_assistant.env
nano my_assistant.env
```

For initial LAN testing over HTTP, use:

```ini
MY_ASSISTANT_LISTEN_ADDR=0.0.0.0:8080
MY_ASSISTANT_TLS_CERT_FILE=
MY_ASSISTANT_TLS_KEY_FILE=
```

Configure the MQTT settings in the same file when MQTT is available:

```ini
MY_ASSISTANT_MQTT_URL=tls://127.0.0.1:8883
MY_ASSISTANT_MQTT_USERNAME=my_assistant_backend
MY_ASSISTANT_MQTT_PASSWORD=replace-me
MY_ASSISTANT_MQTT_CA_FILE=/etc/my_assistant/mqtt/ca.crt
```

Do not use placeholder passwords or commit the deployment environment file.

To register the ESP32-S3-EYE camera that serves MJPEG at
`http://192.168.0.77/stream`, add these settings:

```ini
MY_ASSISTANT_CAMERA_ID=esp32_camera01
MY_ASSISTANT_CAMERA_NAME=ESP32-S3-EYE
MY_ASSISTANT_CAMERA_STREAM_URL=http://192.168.0.77/stream
```

The Pi and the computer viewing the dashboard must be able to reach
`192.168.0.77`. The camera URL is used by the browser for the MJPEG stream, so
it must be reachable from the viewing computer as well. No camera firmware
change is required while `/stream` is working.

## 4. Install and start the service

The release contains the installer. Run it from the extracted release
directory:

```bash
sudo ./install.sh .
```

The installer creates the `my_assistant` system user when necessary, installs
the binary and systemd files, copies the environment file, and enables the
service at boot. The extracted Work directory is only a staging directory; the
running service uses these permanent locations:

- Binary: `/usr/local/bin/my_assistant`
- Service configuration: `/etc/systemd/system/my_assistant.service`
- Environment: `/etc/my_assistant/my_assistant.env`
- Database and application data: `/var/lib/my_assistant`
- Logs: `/var/log/my_assistant`

Therefore, the application continues running after a reboot even if the
release archive or extracted directory is later removed.

## 5. Check the service

```bash
sudo systemctl status my_assistant
sudo journalctl -u my_assistant -f
```

Check the unauthenticated health endpoint from the Pi:

```bash
curl http://127.0.0.1:8080/healthz
```

From another computer on the LAN, open:

```text
http://192.168.1.10:8080
```

Do not use `http://127.0.0.1:8080` on the PC. `127.0.0.1` refers to the PC
itself; use the Raspberry Pi address instead.

If this URL returns `404 page not found`, the Pi is probably running an older
backend binary that did not yet embed the frontend. Rebuild and copy the latest
release from WSL, then reinstall it:

```bash
# WSL
cd /home/louiey/Work/RPI/my_assistant
source ~/.nvm/nvm.sh
./scripts/build-release.sh
scp dist/my_assistant-linux-arm64.tar.gz pi@192.168.1.10:~/Work/
```

```bash
# Raspberry Pi
cd ~/Work
tar -xzf my_assistant-linux-arm64.tar.gz \
  -C my_assistant-linux-arm64 --strip-components=1
cd my_assistant-linux-arm64
sudo ./install.sh .
sudo systemctl restart my_assistant
```

Confirm that the service is running and that the dashboard binary was replaced:

```bash
sudo systemctl status my_assistant
curl -i http://127.0.0.1:8080/
curl -i http://127.0.0.1:8080/healthz
```

The root request should return the frontend HTML. The health request should
return JSON with `{"status":"ok"}`.

If the Pi firewall is enabled, allow the dashboard port:

```bash
sudo ufw allow 8080/tcp
```

## Authentication note

The login page is included and is available at:

```text
http://192.168.1.10:8080
```

There is no default username or password. Login will fail until the first user
is provisioned.

Create the first administrator account after installation. The database path
must be specified because systemd loads `/etc/my_assistant/my_assistant.env`,
but a command run manually does not automatically load that file:

```bash
sudo env MY_ASSISTANT_DATABASE=/var/lib/my_assistant/my_assistant.sqlite3 \
  /usr/local/bin/my_assistant user create
```

The command prompts for an initial administrator username and password, hides
password input, and stores only a bcrypt password hash. After it prints
`user created`, sign in at `http://192.168.1.10:8080`. Do not insert plaintext
passwords into the database or commit credentials to the repository.

After adding or changing camera settings, restart the service and verify that
the camera appears in the authenticated dashboard:

```bash
sudo systemctl restart my_assistant
sudo journalctl -u my_assistant -n 30 --no-pager
curl http://127.0.0.1:8080/healthz
```

Then open the dashboard, log in, and select `Load stream` on the ESP32-S3-EYE
camera card. If sensors update but the camera does not, first test the stream
from the viewing computer:

```bash
curl -I http://192.168.0.77/stream
```

## HTTPS deployment

For deployment beyond the trusted LAN, provision a certificate and private key,
then set both paths in `/etc/my_assistant/my_assistant.env`:

```ini
MY_ASSISTANT_TLS_CERT_FILE=/etc/my_assistant/tls/server.crt
MY_ASSISTANT_TLS_KEY_FILE=/etc/my_assistant/tls/server.key
```

Restart the service after changing the environment:

```bash
sudo systemctl restart my_assistant
sudo systemctl status my_assistant
```

### Short

```bash
# WSL
cd /home/louiey/Work/RPI/my_assistant
source ~/.nvm/nvm.sh
./scripts/build-release.sh dist/my_assistant-linux-arm64-mqtt
scp dist/my_assistant-linux-arm64-mqtt.tar.gz pi@192.168.1.10:~/Work/

# Raspberry Pi
cd ~/Work
tar -xzf my_assistant-linux-arm64.tar.gz \
  -C my_assistant-linux-arm64 --strip-components=1
cd my_assistant-linux-arm64
sudo ./install.sh .
sudo systemctl restart my_assistant

# RPI
cd ~/Work
tar -xzf my_assistant-linux-arm64.tar.gz \
  -C my_assistant-linux-arm64 --strip-components=1
cd my_assistant-linux-arm64

sudo install -Dm755 \
    ~/Work/my_assistant-linux-arm64-mqtt/my_assistant \
    /usr/local/bin/my_assistant

sudo systemctl restart my_assistant
sudo journalctl -u my_assistant -f
```

```bash
Deploy:

  scp dist/my_assistant-linux-arm64-camera-proxy.tar.gz \
    pi@192.168.1.10:~/Work/

  On the Pi:

  cd ~/Work
  mkdir -p my_assistant-linux-arm64-camera-proxy

  tar -xzf my_assistant-linux-arm64-camera-proxy.tar.gz \
    -C my_assistant-linux-arm64-camera-proxy --strip-components=1

  cd my_assistant-linux-arm64-camera-proxy
  sudo cp /etc/my_assistant/my_assistant.env ./my_assistant.env
  sudo ./install.sh .
  sudo systemctl restart my_assistant

  Then press Ctrl+F5 in the browser.
  ```