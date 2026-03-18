# Voice System

Steward has built-in STT (Speech-to-Text) and TTS (Text-to-Speech) support. Voice is used by the Satellite system for JARVIS-style interaction.

## Speech-to-Text (STT)

### Groq Whisper (Recommended)

Fastest option — Whisper large-v3 runs on Groq's LPU hardware.

```yaml
voice:
  stt:
    provider: "groq"
    api_key: "gsk_..."
    model: "whisper-large-v3-turbo"
```

### OpenAI Whisper

```yaml
voice:
  stt:
    provider: "openai"
    api_key: "sk-..."
    model: "whisper-1"
```

### Local whisper.cpp (Offline)

Fully offline — runs on your server's CPU.

```yaml
voice:
  stt:
    provider: "local"
    model: "models/ggml-base.bin"
    binary_path: "whisper-cpp"    # must be in PATH
    language: "auto"              # or "en", "tr", etc.
    threads: 4
```

**Setup:**
```bash
# Build whisper.cpp
git clone https://github.com/ggerganov/whisper.cpp
cd whisper.cpp && make
sudo cp main /usr/local/bin/whisper-cpp

# Download model
bash models/download-ggml-model.sh base
cp models/ggml-base.bin /etc/steward/models/
```

## Text-to-Speech (TTS)

### OpenAI TTS

```yaml
voice:
  tts:
    provider: "openai"
    api_key: "sk-..."
    model: "tts-1"           # or "tts-1-hd" for higher quality
    voice: "nova"            # alloy | echo | fable | onyx | nova | shimmer
```

### ElevenLabs

Premium quality, multilingual voices.

```yaml
voice:
  tts:
    provider: "elevenlabs"
    api_key: "sk_..."
    model: "eleven_multilingual_v2"
    voice: "X5CGTTx85DmIuopBFHlz"   # voice ID from ElevenLabs
```

Get voice IDs from the [ElevenLabs Voice Library](https://elevenlabs.io/voice-library).

### Piper (Offline)

Fully offline TTS using ONNX models.

```yaml
voice:
  tts:
    provider: "piper"
    model: "models/en_US-lessac-medium.onnx"
    binary_path: "piper"
```

**Setup:**
```bash
# Install Piper
wget https://github.com/rhasspy/piper/releases/latest/download/piper_linux_x86_64.tar.gz
tar xzf piper_linux_x86_64.tar.gz
sudo cp piper /usr/local/bin/

# Download voice model
wget https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/lessac/medium/en_US-lessac-medium.onnx
wget https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_US/lessac/medium/en_US-lessac-medium.onnx.json
```

## Audio Flow

```
Satellite Mic → WebSocket → Server STT → Text → LLM → Response Text → TTS → WebSocket → Satellite Speaker
```

Voice requires the [Satellite system](Satellite.md) to be set up for end-to-end audio interaction.
