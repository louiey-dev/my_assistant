echo "Build and deploy"
./scripts/build-release.sh
scp dist/my_assistant-linux-arm64.tar.gz pi@192.168.1.10:~/Work/
echo "Done"

