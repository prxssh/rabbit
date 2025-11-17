# Rabbit BitTorrent Client - UI Design Specification
## Minimalist Monochrome Design

---

## Design Philosophy
- **Monochrome palette**: Pure blacks, grays, and whites only
- **Monospace typography**: Consistent use of mono fonts throughout
- **Brutalist aesthetics**: Clean lines, sharp edges, high information density
- **Terminal-inspired**: Code/terminal-like interface for power users
- **Minimal icons**: Use ASCII/Unicode characters instead of icon fonts

---

## Color Palette

```
Background Layers:
  bg-primary:   #0a0a0a (darkest - main background)
  bg-secondary: #111111 (panels)
  bg-tertiary:  #1a1a1a (selected items)
  bg-hover:     #151515 (hover states)
  bg-elevated:  #252525 (modals, dropdowns)

Borders:
  border-1: #1a1a1a (subtle)
  border-2: #222222 (normal)
  border-3: #333333 (strong)
  border-4: #444444 (active)

Text:
  text-primary:   #dddddd (main text)
  text-secondary: #cccccc (secondary info)
  text-tertiary:  #aaaaaa (labels)
  text-muted:     #888888 (hints, disabled)
  text-disabled:  #666666

Accents:
  success: #88ff88 (completed, seeding)
  error:   #ff8888 (errors, failed)
  warning: #ffaa88 (paused, attention needed)
  info:    #aaaaaa (downloading, in-progress)
```

---

## Main Application Layout

```
┌────────────────────────────────────────────────────────────────────────────────────┐
│ rabbit                                              ↓ 5.2 MB/s  ↑ 1.8 MB/s  ⚙  ─ □ × │ TOP BAR
├──────┬─────────────────────────────────────────────────────────────────────────────┤
│      │  [+] add   [‖] pause [▶] resume [×] remove  │  [⌕] search           TOOLBAR│
├──────┼─────────────────────────────────────────────────────────────────────────────┤
│      │  [name ↓]  [size]  [progress]  [↓speed]  [↑speed]  [eta]  [ratio]          │ COLUMN
│FILTER│  ─────────────────────────────────────────────────────────────────────────  │ HEADERS
│      │                                                                              │
│■ all │  ubuntu-24.04-desktop-amd64.iso                                             │
│  42  │  5.8 GB  ████████████████░░░░  75.3%  ↓2.1 MB/s  ↑850 KB/s  00:14:32  1.2  │
├──────┤  ─────────────────────────────────────────────────────────────────────────  │
│      │                                                                              │
│↓ dwnl│  archlinux-2024.01.01-x86_64.iso                                            │
│  12  │  900 MB  ████████░░░░░░░░░░░░  45.2%  ↓1.5 MB/s  ↑120 KB/s  00:06:15  0.1  │
├──────┤  ─────────────────────────────────────────────────────────────────────────  │ TORRENT
│      │                                                                              │ LIST
│↑ seed│  debian-12.4.0-amd64-netinst.iso                                            │
│  8   │  650 MB  ████████████████████  100%   ↓0 B/s     ↑2.8 MB/s  --       5.4  │
├──────┤  ─────────────────────────────────────────────────────────────────────────  │
│      │                                                                              │
│✓ done│  linuxmint-21.3-cinnamon-64bit.iso                                          │
│  18  │  2.8 GB  ████████████████████  100%   ↓0 B/s     ↑0 B/s     complete  0.0  │
├──────┤  ─────────────────────────────────────────────────────────────────────────  │
│      │                                                                              │
│‖ paus│  [SELECTED] kali-linux-2024.1-live-amd64.iso                                │
│  3   │  3.6 GB  ██████░░░░░░░░░░░░░░  35.8%  ↓0 B/s     ↑0 B/s     paused   0.5  │
├──────┼──────────────────────────────────────────────────────────────────────────── ┤
│      │  ┌─[ info ]──[ files ]──[ peers ]──[ trackers ]──[ pieces ]─────────────┐  │
│! err │  │                                                                        │  │ DETAIL
│  1   │  │  name:     kali-linux-2024.1-live-amd64.iso                          │  │ PANEL
│      │  │  hash:     a3f5c8d7e9b2a1c4f6e8d9b7a5c3e1f2d4b6a8c9e               │  │
│      │  │  size:     3.6 GB (3,867,148,288 bytes)                             │  │
│      │  │  created:  2024-03-15 14:23:45                                       │  │
│      │  │  comment:  Official Kali Linux release                               │  │
│      │  │                                                                        │  │
│      │  │  status:   paused by user                                            │  │
│      │  │  down:     1.29 GB (35.8%) at 0 B/s                                  │  │
│      │  │  up:       645 MB (ratio 0.5) at 0 B/s                               │  │
│      │  │  avail:    95.2% (from 12/15 peers)                                  │  │
│      │  │  eta:      paused                                                     │  │
│      │  │                                                                        │  │
│      │  │  path:     /home/user/Downloads/torrents/                            │  │
│      │  └────────────────────────────────────────────────────────────────────────┘  │
├──────┴─────────────────────────────────────────────────────────────────────────────┤
│ ready  │  42 torrents  │  12 downloading  │  8 seeding  │  DHT: 1,234 nodes         │ STATUS
└────────────────────────────────────────────────────────────────────────────────────┘ BAR
```

---

## Component Breakdown

### 1. TOP BAR (48px height)
```
┌────────────────────────────────────────────────────────────────────────────┐
│ rabbit                             ↓ 5.2 MB/s  ↑ 1.8 MB/s  ⚙  ─ □ ×        │
└────────────────────────────────────────────────────────────────────────────┘

Left:  App name/logo "rabbit" in monospace
Right: Download speed │ Upload speed │ Settings │ Window controls
```

**Features:**
- Real-time speed indicators with Unicode arrows
- Global settings button (⚙)
- Window controls for frameless design
- Click app name to show "about" dialog

---

### 2. TOOLBAR (40px height)
```
┌──────────────────────────────────────────────────────────────────────────┐
│ [+] add  [‖] pause [▶] resume [×] remove  │  [⌕] search   [≡] sort      │
└──────────────────────────────────────────────────────────────────────────┘

Left:  Torrent actions (context-sensitive, disable when no selection)
Right: Search torrents │ Sort options
```

**Actions:**
- `[+] add` - Add torrent (file/magnet/URL)
- `[‖] pause` - Pause selected torrent(s)
- `[▶] resume` - Resume selected torrent(s)
- `[×] remove` - Remove selected torrent(s)
- `[⌕] search` - Open torrent search panel
- `[≡] sort` - Sort dropdown menu

**Keyboard Shortcuts:**
- `Ctrl+O` - Add torrent
- `Space` - Pause/Resume selected
- `Delete` - Remove selected
- `Ctrl+F` - Search torrents
- `Ctrl+A` - Select all
- `Ctrl+,` - Settings

---

### 3. FILTER SIDEBAR (180px width)

```
┌───────────┐
│  FILTERS  │
├───────────┤
│           │
│ ■ all  42 │ ← all torrents
│           │
│ ↓ dwnl 12 │ ← downloading
│           │
│ ↑ seed  8 │ ← seeding
│           │
│ ✓ done 18 │ ← completed
│           │
│ ‖ paus  3 │ ← paused
│           │
│ ! err   1 │ ← errors
│           │
├───────────┤
│   LABELS  │
├───────────┤
│           │
│ # linux   │
│ # iso     │
│ # work    │
│ # media   │
│           │
│ [+ label] │
│           │
└───────────┘
```

**Filter Categories:**
- `■ all` - All torrents
- `↓ downloading` - Currently downloading
- `↑ seeding` - Currently seeding (100% complete, uploading)
- `✓ completed` - Downloaded but not seeding
- `‖ paused` - Paused by user
- `! error` - Errors/warnings

**Custom Labels:**
- User-created tags/labels for organization
- Click to filter by label
- Drag torrents to apply labels

---

### 4. TORRENT LIST (Main Content Area)

#### Column Headers
```
┌──────────────┬─────────┬──────────┬─────────┬─────────┬──────────┬───────┐
│ name ↓       │ size    │ progress │ ↓speed  │ ↑speed  │ eta      │ ratio │
└──────────────┴─────────┴──────────┴─────────┴─────────┴──────────┴───────┘
```

**Sortable Columns:**
- **name** - Torrent name (↑↓ sortable)
- **size** - Total size in bytes
- **progress** - Download progress (0-100%)
- **↓speed** - Download speed
- **↑speed** - Upload speed
- **eta** - Estimated time remaining
- **ratio** - Upload/Download ratio
- **peers** - Connected peers (optional column)
- **seeds** - Available seeds (optional column)
- **added** - Date added (optional column)

#### Torrent Item (Compact View)
```
┌──────────────────────────────────────────────────────────────────────────┐
│ ubuntu-24.04-desktop-amd64.iso                                           │
│ 5.8 GB  ████████████████░░░░  75.3%  ↓2.1 MB/s  ↑850 KB/s  00:14:32 1.2 │
└──────────────────────────────────────────────────────────────────────────┘
```

#### Torrent Item (Selected/Expanded)
```
┌──────────────────────────────────────────────────────────────────────────┐
│ [SELECTED] kali-linux-2024.1-live-amd64.iso                      [⚙][×] │
│ 3.6 GB  ██████░░░░░░░░░░░░░░  35.8%  ↓0 B/s  ↑0 B/s  paused  0.5        │
│ ─────────────────────────────────────────────────────────────────────── │
│ 1,290 MB of 3.6 GB (35.8%) • 12 of 15 peers • Availability: 95.2%       │
└──────────────────────────────────────────────────────────────────────────┘
```

**Progress Bar States:**
- `████████████████████` - Completed (100%)
- `████████████░░░░░░░░` - In progress
- `--------------------` - Paused (hollow/dashed)
- `!!!!!!!!!!!!!!!!!!!!` - Error state

**Status Indicators:**
- Color coding via text color:
  - Downloading: info color (gray)
  - Seeding: success color (green)
  - Paused: warning color (yellow/amber)
  - Error: error color (red)
  - Completed: success color (green)

---

### 5. DETAIL PANEL (Bottom, expandable, 50% height when open)

```
┌─[ info ]──[ files ]──[ peers ]──[ trackers ]──[ pieces ]────────────────┐
│                                                                           │
│  [INFO TAB CONTENT]                                                      │
│                                                                           │
└───────────────────────────────────────────────────────────────────────────┘
```

#### Tab 1: INFO (General Information)
```
┌───────────────────────────────────────────────────────────────────────────┐
│ name:     ubuntu-24.04-desktop-amd64.iso                                 │
│ hash:     a3f5c8d7e9b2a1c4f6e8d9b7a5c3e1f2d4b6a8c9e1f3a5b7c9d           │
│ size:     5.8 GB (6,241,556,480 bytes)                                   │
│ pieces:   2,976 × 2 MB (last piece: 1.5 MB)                              │
│ created:  2024-03-15 14:23:45                                            │
│ added:    2024-11-17 09:15:22                                            │
│ comment:  Ubuntu 24.04 LTS "Noble Numbat" Official Release               │
│                                                                           │
│ status:   downloading                                                    │
│ down:     4.36 GB (75.3%) at 2.1 MB/s                                    │
│ up:       5.23 GB (ratio 1.2) at 850 KB/s                                │
│ avail:    100% (from 45/128 peers, 89 seeds)                             │
│ eta:      00:14:32 (estimated)                                           │
│ wasted:   12.3 MB (hashfails, retries)                                   │
│                                                                           │
│ path:     /home/user/Downloads/torrents/                                 │
│ privacy:  public torrent                                                 │
│ source:   https://releases.ubuntu.com/24.04/                             │
└───────────────────────────────────────────────────────────────────────────┘
```

#### Tab 2: FILES (File Tree with Priorities)
```
┌───────────────────────────────────────────────────────────────────────────┐
│  [⊡] [name]                                                [size] [%] [pri]│
│  ─────────────────────────────────────────────────────────────────────── │
│  [☑] ubuntu-24.04-desktop-amd64.iso                      5.8 GB 100%  ███ │
│                                                                           │
│  For multi-file torrents:                                                │
│  [☑] ┬ debian-complete/                                  15.2 GB 45%      │
│  [ ] ├─ README.txt                                         2.1 KB 100% ███ │
│  [☑] ├─ debian-12.4.0-amd64-DVD-1.iso                     3.7 GB 100% ███ │
│  [☑] ├─ debian-12.4.0-amd64-DVD-2.iso                     3.7 GB  75%  ██  │
│  [ ] ├─ debian-12.4.0-amd64-DVD-3.iso                     3.7 GB   0%  ─   │
│  [ ] └─ debian-12.4.0-amd64-DVD-4.iso                     4.1 GB   0%  skip│
│                                                                           │
│  Priority: [skip] [─] low  [██] normal  [███] high                       │
│  Right-click or select to change priority                                │
└───────────────────────────────────────────────────────────────────────────┘
```

**Features:**
- Checkboxes to select files
- Tree view for directories
- Individual file progress
- Priority controls: skip, low, normal, high
- Right-click context menu for bulk operations
- Show/hide skipped files toggle

#### Tab 3: PEERS (Connected Peers)
```
┌────────────────────────────────────────────────────────────────────────────┐
│ [ip address]      [client]  [%]  [↓speed]  [↑speed]  [flags]              │
│ ──────────────────────────────────────────────────────────────────────────│
│ 192.168.1.105     qB 4.6.2  100%  125 KB/s   0 B/s    DHE   [disconnect]  │
│ 45.123.67.89      uT 3.5.5   87%    0 B/s   45 KB/s   DHE   [disconnect]  │
│ 203.45.12.34      DE 2.1.1   92%  256 KB/s  120 KB/s  DHEI  [disconnect]  │
│ 172.16.254.12     rT 0.9.8   45%  512 KB/s   0 B/s    DHE   [disconnect]  │
│ 88.77.66.55       TR 4.0.5  100%    0 B/s   850 KB/s  DHEI  [disconnect]  │
│                                                                            │
│ Total: 45 peers (12 downloading, 33 seeding) • 89 seeds in swarm          │
│                                                                            │
│ Flags: D=downloading U=uploading H=handshake E=encrypted I=incoming        │
└────────────────────────────────────────────────────────────────────────────┘
```

**Columns:**
- IP address (with optional GeoIP country flag)
- Client type & version
- Peer's download completion %
- Download speed from peer
- Upload speed to peer
- Connection flags
- Disconnect button

**Features:**
- Real-time peer statistics
- Ban/unban peers
- Copy IP address
- Show peer history

#### Tab 4: TRACKERS (Tracker Management)
```
┌────────────────────────────────────────────────────────────────────────────┐
│ [tier] [tracker url]                        [status]    [seeds] [peers]    │
│ ──────────────────────────────────────────────────────────────────────────│
│   1    udp://tracker.openbittorrent.com     ✓ working      89      45     │
│   1    udp://tracker.opentrackr.org         ✓ working      89      45     │
│   2    http://tracker.ubuntu.com:6969       ✓ working      89      45     │
│   2    http://torrent.ubuntu.com:6969       ! timeout       -       -     │
│   3    udp://tracker.coppersurfer.tk        × offline       -       -     │
│                                                                            │
│ Next announce in: 00:12:45                                                │
│                                                                            │
│ [+ add tracker]  [× remove selected]  [⟳ force announce]                 │
└────────────────────────────────────────────────────────────────────────────┘
```

**Features:**
- Multi-tier tracker list
- Add/remove trackers
- Force reannounce
- Edit tracker URLs
- Tracker tier management
- Status indicators: ✓ working, ! warning, × offline

#### Tab 5: PIECES (Visual Piece Map / Heatmap)
```
┌────────────────────────────────────────────────────────────────────────────┐
│ Piece Availability Heatmap (2,976 pieces × 2 MB)                          │
│                                                                            │
│ ████████████████████████████░░░░░░░░                                       │
│ ░░░░░░░░░░                                                                 │
│                                                                            │
│ Legend: █ complete   ▓ downloading   ░ pending   ─ not wanted             │
│                                                                            │
│ Completed: 2,241 pieces (75.3%)                                           │
│ In-flight: 89 pieces (3.0%)                                               │
│ Pending:   646 pieces (21.7%)                                             │
│                                                                            │
│ Strategy: [rarest-first ▼]  [⚙ advanced]                                 │
└────────────────────────────────────────────────────────────────────────────┘
```

**Features:**
- Visual block/piece completion map
- Color-coded states
- Hover to see piece details
- Download strategy selector
- Piece size info

---

### 6. STATUS BAR (32px height)
```
┌────────────────────────────────────────────────────────────────────────────┐
│ ready  │  42 torrents  │  12 downloading  │  8 seeding  │  DHT: 1,234 nodes│
└────────────────────────────────────────────────────────────────────────────┘
```

**Sections:**
- Current status message (left)
- Total torrent count
- Active downloads count
- Active seeds count
- DHT nodes count
- Storage space indicator (optional)

**Status Messages:**
- "ready" - Idle
- "downloading ubuntu-24.04..." - Active download
- "error: tracker unreachable" - Error state
- "completed: archlinux-2024.01.01..." - Success

---

## Additional Dialogs & Panels

### ADD TORRENT DIALOG
```
┌───────────────────────────────────────────────────────────────┐
│ add torrent                                               [×] │
├───────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌─( ) torrent file                                          │
│  │  [browse...] or drag & drop .torrent file                 │
│  │                                                            │
│  └─(•) magnet link                                            │
│     [magnet:?xt=urn:btih:a3f5c8d7e9b2a1c4f6e8...]           │
│                                                               │
│  ┌─────────────────────────────────────────────────────────  │
│  │ download to:                                              │
│  │ [/home/user/Downloads/torrents/        ] [browse...]     │
│  │                                                            │
│  │ [ ] start immediately                                     │
│  │ [☑] sequential download                                   │
│  │ [ ] remember location                                     │
│  └─────────────────────────────────────────────────────────  │
│                                                               │
│                                      [cancel]  [add torrent]  │
└───────────────────────────────────────────────────────────────┘
```

### SEARCH TORRENTS PANEL
```
┌───────────────────────────────────────────────────────────────────┐
│ search torrents                                               [×] │
├───────────────────────────────────────────────────────────────────┤
│                                                                   │
│  [ubuntu 24.04________________________]  [⌕ search]              │
│  provider: [piratebay ▼]   category: [all ▼]   sort: [seeds ▼]  │
│                                                                   │
│  ┌─────────────────────────────────────────────────────────────  │
│  │ [name]                              [size]  [se] [le] [date]  │
│  │ ───────────────────────────────────────────────────────────── │
│  │ Ubuntu 24.04 LTS Desktop AMD64      5.8 GB   89   45  Mar 15 │
│  │ Ubuntu 24.04 Server AMD64           2.5 GB   45   12  Mar 15 │
│  │ Ubuntu 24.04 All Editions          18.2 GB   12    3  Mar 16 │
│  │                                                                │
│  └─────────────────────────────────────────────────────────────  │
│                                                                   │
│  [1] [2] [3] ... [12]                         [download selected]│
└───────────────────────────────────────────────────────────────────┘
```

### SETTINGS PANEL
```
┌───────────────────────────────────────────────────────────────────┐
│ settings                                                      [×] │
├──────────┬────────────────────────────────────────────────────────┤
│          │                                                        │
│ general  │  downloads                                            │
│ network  │  ┌─────────────────────────────────────────────────── │
│ speed    │  │ default save path:                                │
│ advanced │  │ [/home/user/Downloads/torrents/] [browse...]      │
│ ui       │  │                                                    │
│          │  │ [☑] create subfolder for multi-file torrents      │
│          │  │ [☑] start torrents immediately                    │
│          │  │ [ ] move completed to: [_____________] [browse...]│
│          │  └─────────────────────────────────────────────────── │
│          │                                                        │
│          │  behavior                                             │
│          │  ┌─────────────────────────────────────────────────── │
│          │  │ [ ] minimize to system tray                       │
│          │  │ [☑] confirm on remove torrent                     │
│          │  │ [ ] confirm on exit with active torrents          │
│          │  └─────────────────────────────────────────────────── │
│          │                                                        │
├──────────┴────────────────────────────────────────────────────────┤
│                                           [cancel]  [save changes]│
└───────────────────────────────────────────────────────────────────┘
```

**Settings Categories:**
1. **General** - Download paths, behavior
2. **Network** - Ports, connections, DHT, encryption
3. **Speed** - Upload/download limits, scheduling
4. **Advanced** - Cache, disk I/O, logging
5. **UI** - Theme (dark/darker/darkest), font size, columns

---

## Contextual Features

### Right-Click Context Menu (Torrent Item)
```
┌──────────────────────┐
│ ▶ resume             │
│ ‖ pause              │
│ ⊙ force resume       │
├──────────────────────┤
│ ⚙ properties         │
│ 📁 open folder       │
│ 📋 copy magnet link  │
│ 📋 copy hash         │
├──────────────────────┤
│ ↑ move to top        │
│ ↓ move to bottom     │
├──────────────────────┤
│ 🏷 add label...      │
├──────────────────────┤
│ × remove             │
│ × remove & delete    │
└──────────────────────┘
```

### Keyboard Shortcuts Overview
```
GLOBAL
  Ctrl+O        add torrent
  Ctrl+F        search torrents
  Ctrl+,        settings
  Ctrl+Q        quit
  Ctrl+A        select all
  Escape        deselect / close dialog

TORRENT LIST
  Enter         toggle details panel
  Space         pause/resume selected
  Delete        remove selected
  ↑↓            navigate list
  Ctrl+↑↓       move torrent priority

DETAILS PANEL
  Tab           next tab
  Shift+Tab     previous tab
```

---

## Responsive Behavior

### Minimum Window Size: 800×600

### Small Window (< 1000px width)
- Hide sidebar (show as hamburger menu)
- Collapse toolbar labels (icons only)
- Reduce detail panel to single column
- Hide optional columns (ratio, eta)

### Medium Window (1000-1400px)
- Full sidebar visible
- Full toolbar
- Standard layout

### Large Window (> 1400px)
- Wider detail panel
- Optional: side-by-side details+list view
- More visible columns

---

## Animation & Transitions

**Principle: Subtle, fast transitions only**

- Button hover: 150ms ease
- Panel expand/collapse: 200ms ease-out
- List item selection: instant (no transition)
- Progress bar: smooth update (CSS transition on width)
- Modal open: 200ms fade+scale

**NO animations for:**
- Speed changes
- Progress updates
- List sorting/filtering

---

## Data Density

**High information density preferred:**
- Compact spacing (4px base unit)
- No wasted whitespace
- Small fonts (13-14px base)
- Tight line heights (1.2-1.5)
- Tab-based details (not accordion)
- Collapsible sections where appropriate

---

## Typography Scale

```
Display:      24px  (app title, large headers)
Heading:      18px  (panel titles)
Subheading:   16px  (section headers)
Body:         14px  (primary content)
Small:        13px  (secondary info, labels)
Caption:      11px  (hints, footnotes)

Line Heights:
  Tight:    1.2   (compact lists, data)
  Normal:   1.5   (readable text)
  Relaxed:  1.75  (long-form content)

Weights:
  Normal:   400
  Medium:   500
  Semibold: 600
```

---

## Icon Set (ASCII/Unicode)

```
Actions:
  +  add
  ×  close/remove
  ⚙  settings
  ⌕  search
  ⟳  refresh/reload
  ▶  play/resume
  ‖  pause
  ■  stop
  ↓  download
  ↑  upload/seed
  ⊙  force

Status:
  ✓  success/complete
  !  warning
  ×  error
  ─  paused/inactive
  …  loading

Navigation:
  ↑↓  arrows
  ◀▶  left/right
  ⌃⌄  collapse/expand

Files:
  📁  folder
  📄  file
  📋  copy

UI:
  ☑  checkbox checked
  ☐  checkbox unchecked
  (•) radio selected
  ( ) radio unselected
  [▼] dropdown
```

---

## Feature Completeness Checklist

### Core Features
- [x] Add torrents (file, magnet, URL)
- [x] Remove torrents (with/without data)
- [x] Pause/resume torrents
- [x] Torrent list with sorting/filtering
- [x] Real-time speed indicators
- [x] Progress tracking

### Detail Views
- [x] Info tab (metadata, stats)
- [x] Files tab (individual files, priorities)
- [x] Peers tab (connected peers, stats)
- [x] Trackers tab (tracker management)
- [x] Pieces tab (visual completion map)

### Search & Discovery
- [x] Torrent search interface
- [x] Multiple search providers
- [x] Search result sorting

### Organization
- [x] Filter sidebar (status categories)
- [x] Custom labels/tags
- [x] Multi-column sorting
- [x] Queue management

### Settings & Configuration
- [x] Download paths
- [x] Speed limits (global, per-torrent)
- [x] Connection settings
- [x] DHT, PEX, encryption
- [x] UI preferences

### Advanced Features
- [ ] RSS feed support
- [ ] Scheduler (time-based limits)
- [ ] IP filtering
- [ ] Bandwidth allocation
- [ ] Sequential download
- [ ] Super seeding
- [ ] Torrent creation

---

## Implementation Notes

### Component Structure
```
App.svelte (main layout)
├── TopBar.svelte (app header)
├── EnhancedToolbar.svelte (actions)
├── Sidebar.svelte (filters)
├── TorrentList.svelte (main content)
│   └── TorrentItem.svelte (individual torrent)
├── DetailPanel.svelte (bottom panel)
│   ├── InfoTab.svelte
│   ├── FilesTab.svelte
│   ├── PeersTab.svelte
│   ├── TrackersTab.svelte
│   └── PiecesTab.svelte
├── StatusBar.svelte (footer)
└── Dialogs
    ├── AddTorrentDialog.svelte
    ├── SearchPanel.svelte
    ├── SettingsPanel.svelte
    └── ConfirmDialog.svelte
```

### State Management
- Torrent list state (array of torrents)
- Selected torrent ID(s)
- Active filter
- Sort order
- UI preferences (collapsed panels, column widths)
- Global stats (speeds, DHT nodes)

### Real-time Updates
- Poll backend every 2 seconds for:
  - Torrent stats (progress, speeds)
  - Peer list
  - Tracker status
- WebSocket alternative for push updates

---

## Accessibility

- Keyboard navigation for all features
- ARIA labels for screen readers
- Focus indicators (subtle border)
- High contrast monochrome palette
- Scalable fonts
- Clear visual hierarchy

---

## Platform Considerations

### Wails-specific
- Frameless window with custom titlebar
- Native file picker integration
- System tray integration
- Native notifications
- Deep link handling (magnet://)

### Cross-platform
- Works on Linux, macOS, Windows
- Respects OS conventions (keybindings)
- Native look via monochrome minimalism

---

This design provides a complete, professional BitTorrent client UI with all expected features while maintaining a strict minimalist, monochrome aesthetic using monospace fonts throughout.
