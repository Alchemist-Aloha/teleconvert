# HandBrake JSON to CLI Conversion

## Original JSON Job Configuration

```json
{
  "Audio": {
    "AudioList": [{
      "Bitrate": 160,
      "Encoder": "fdk_aac",
      "Mixdown": "stereo",
      "Track": 0
    }],
    "FallbackEncoder": "fdk_aac"
  },
  "Destination": {
    "ChapterMarkers": false,
    "File": "/storage/data/namer/hucows.26.04.04.roxy.goat.milker.m4v",
    "Mux": "m4v",
    "Options": {"Optimize": true}
  },
  "Filters": {
    "FilterList": [
      {
        "ID": 11,
        "Settings": {"mode": 0}
      },
      {
        "ID": 20,
        "Settings": {
          "height": 1080,
          "width": 1920,
          "crop-bottom": 0,
          "crop-left": 0,
          "crop-right": 0,
          "crop-top": 0
        }
      }
    ]
  },
  "Source": {
    "Path": "/storage/intelssd/torrents/HuCows.26.04.04.Roxy.Goat.Milker.XXX.1080p.MP4-FETiSH[XC]/hucows.26.04.04.roxy.goat.milker.mp4",
    "Title": 1,
    "Range": {
      "Type": "chapter",
      "Start": 1,
      "End": 1
    }
  },
  "Video": {
    "Encoder": "x265",
    "Preset": "fast",
    "Profile": "auto",
    "Level": "auto",
    "Quality": 27.0
  }
}
```

## Converted HandBrakeCLI Command

```bash
HandBrakeCLI \
  -i {{.Input}} \
  -o {{.Output}} \
  -e x265 \
  -q 27.0 \
  --preset fast \
  -a 1 \
  -E fdk_aac \
  -B 160 \
  -6 stereo \
  -f m4v \
  --deinterlace \
  -w 1920 \
  -l 1080 \
  -O
```

## Parameter Mapping

| JSON Parameter | CLI Flag | Value | Purpose |
|---|---|---|---|
| `Source.Path` | `-i {{.Input}}` | Input file | Source video file |
| `Destination.File` | `-o {{.Output}}` | Output file | Destination video file |
| `Video.Encoder` | `-e` | x265 | Video codec |
| `Video.Quality` | `-q` | 27.0 | Quality (0-51 for x265, lower = better) |
| `Video.Preset` | `--preset` | fast | Encoding speed/quality tradeoff |
| `Audio.AudioList[0].Track` | `-a` | 1 | Audio track to encode |
| `Audio.AudioList[0].Encoder` | `-E` | fdk_aac | Audio codec |
| `Audio.AudioList[0].Bitrate` | `-B` | 160 | Audio bitrate in kbps |
| `Audio.AudioList[0].Mixdown` | `-6` | stereo | Audio channels |
| `Destination.Mux` | `-f` | m4v | Container format |
| `Filters.FilterList[0]` (ID 11) | `--deinterlace` | - | Deinterlace filter (mode 0 = default) |
| `Filters.FilterList[1]` (ID 20) | `-w -l` | 1920x1080 | Scale to resolution (no crop) |
| `Destination.Options.Optimize` | `-O` | - | Optimize for iTunes/QuickTime |
| `Video.Profile` | (omitted) | auto | Use default profile |
| `Video.Level` | (omitted) | auto | Use default level |

## Equivalent One-Liner

```bash
HandBrakeCLI -i {{.Input}} -o {{.Output}} -e x265 -q 27.0 --preset fast -a 1 -E fdk_aac -B 160 -6 stereo -f m4v --deinterlace -w 1920 -l 1080 -O
```

## Teleconvert Config Usage

```yaml
nodes:
  - name: "home-server"
    address: "localhost"
    max_concurrent: 1
    command: "HandBrakeCLI -i {{.Input}} -o {{.Output}} -e x265 -q 27.0 --preset fast -a 1 -E fdk_aac -B 160 -6 stereo -f m4v --deinterlace -w 1920 -l 1080 -O"
    tmp_dir: "/tmp/teleconvert"
```

## HandBrakeCLI Flag Reference

| Flag | Meaning | Examples |
|------|---------|----------|
| `-i` | Input file path | `-i input.mp4` |
| `-o` | Output file path | `-o output.m4v` |
| `-e` | Video encoder | `-e x265`, `-e x264`, `-e mpeg2` |
| `-q` | Quality/CRF (for x265) | `-q 27.0` (0-51, lower=better) |
| `--preset` | Encoding speed | `ultrafast`, `superfast`, `veryfast`, `faster`, `fast`, `medium`, `slow`, `slower`, `veryslow` |
| `-a` | Audio track | `-a 1` (first track), `-a 1,2` (multiple) |
| `-E` | Audio encoder | `-E aac`, `-E fdk_aac`, `-E libopus`, `-E flac` |
| `-B` | Audio bitrate | `-B 160` (kbps) |
| `-6` | Audio mixdown | `-6 stereo`, `-6 mono`, `-6 5point1`, `-6 7point1` |
| `-f` | Container format | `-f m4v`, `-f mkv`, `-f mp4` |
| `--deinterlace` | Deinterlace filter | Removes interlacing artifacts |
| `-w` | Width | `-w 1920` |
| `-l` | Height | `-l 1080` |
| `-O` | Optimize for streaming | Optimizes for iTunes/QuickTime |

## Performance Notes

- **Preset: fast** → Good speed/quality balance (~3-5x slower than ultrafast, ~2x faster than medium)
- **Quality: 27.0** → Moderate quality (typical range 18-28 for x265)
- **Audio: 160 kbps** → Standard stereo audio quality
- **Resolution: 1920x1080** → 1080p Full HD
- **Deinterlace** → Useful for broadcast/interlaced source material
- **iTunes Optimization** → Ensures compatibility with Apple devices

## Testing the Command

```bash
# Test locally to verify syntax before adding to config
HandBrakeCLI -i test-input.mp4 -o test-output.m4v -e x265 -q 27.0 --preset fast -a 1 -E fdk_aac -B 160 -6 stereo -f m4v --deinterlace -w 1920 -l 1080 -O

# Monitor progress
watch -n 1 'ls -lh test-output.m4v'
```

## Customization Examples

### Lower quality, faster encoding:
```bash
HandBrakeCLI -i {{.Input}} -o {{.Output}} -e x265 -q 32.0 --preset faster -a 1 -E fdk_aac -B 128 -6 stereo -f m4v
```

### Higher quality, slower encoding:
```bash
HandBrakeCLI -i {{.Input}} -o {{.Output}} -e x265 -q 20.0 --preset slow -a 1 -E fdk_aac -B 192 -6 5point1 -f m4v -O
```

### Keep original audio stream:
```bash
HandBrakeCLI -i {{.Input}} -o {{.Output}} -e x265 -q 27.0 --preset fast -a 1 -E copy -f m4v -O
```

---

**Status**: ✅ Updated in `test-config.yaml` home-server node
