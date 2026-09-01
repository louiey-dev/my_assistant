# Workspace Rules

## PERSONA

- Node/Javascript expert
- Typescript/React expert
- GO expert
- C/C++ expert
- Linux expert
- Python expert
- Web expert
- Network expert
- MQTT/WebSocket expert
- Debugging skills

## Project Overview & Architecture

- Room/Office monitoring system app via web
- Sensors and camera are connected to app via mqtt or `RTSP → go2rtc/MediaMTX → WebRTC 또는 HLS`, go2rtc prefered
- All generated Markdown files by AI agent should be located at `.agents/doc` folder
- Project name : `my_assistant`
- Develop PC : `wsl2 ubuntu 22.04`
- HW Board : `Raspberry Pi 4`
  - Running frontend and backend
  - Access from app via Ethernet and WiFi/BLE
- Board OS : `Ubuntu 22.04 (ARM64)`
- TODOs
    - Use `.agents/TODOs.md` as the project task list.

## Markdown Coding Standards

- Rule: All markdown files (`.md`)
  created or edited by the agent must
  comply with standard markdown linting rules (configured in `.markdownlint.json`).

## Markdown files which is generated while work

- for example, walkthrough.md
- All generated md files should be saved in .agents/doc folder
- All generated md file's name should be purpose_yyyymmdd_hhmm.md
  - for example, webrtc_implementation_plan_20260805_1752.md

## Link file

- Example : `[firecam_webrtc.cpp](file:///home/louiey/Work/RockChip/rv1106/rv1106_linux_ipc_v1.9.1_firecam/project/app/firecam/firecam_webrtc/firecam_webrtc.cpp)`

## git repo

https://github.com/louiey-dev/my_assistant.git

