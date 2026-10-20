# Hướng dẫn cài đặt và sử dụng imapsync MCP

`imapsync-mcp` cho phép Claude (Claude Code, Claude Desktop hoặc MCP client
khác) điều khiển trang chuyển mail **https://sync.wppanel.io.vn** bằng lời nói
thường: chuyển mail, theo dõi tiến độ, đọc log lỗi, chạy batch, đặt lịch…

Nó chạy **trên máy của bạn**, gọi sang server qua API bằng một API key. Việc
chuyển mail vẫn diễn ra trên server — tắt Claude giữa chừng thì job vẫn chạy
tiếp.

---

## 1. Chuẩn bị binary

Đã có Go (1.25 trở lên):

```bash
cd web/mcp
make build          # tạo ./imapsync-mcp cho máy hiện tại
./imapsync-mcp --version
```

Cần đưa cho người khác dùng (không cài Go):

```bash
make dist           # tạo dist/ cho macOS (arm64, amd64), Linux, Windows
```

Gửi file phù hợp với máy của họ, rồi trên macOS/Linux: `chmod +x imapsync-mcp-*`.

> macOS có thể chặn file tải về từ nơi khác. Nếu bị báo "cannot be opened":
> `xattr -d com.apple.quarantine ./imapsync-mcp-darwin-arm64`

---

## 2. Tạo API key

1. Đăng nhập https://sync.wppanel.io.vn → **API Access**.
2. Ở **API keys**: đặt tên (vd. `claude-mcp`), chọn scope, bấm **Create key**.
3. **Copy key ngay** (dạng `isk_...`) — key chỉ hiện đúng một lần.

Chọn scope theo việc muốn giao cho Claude:

| Mục đích | Scope cần chọn |
|---|---|
| Chỉ xem: tiến độ, log, lịch sử | `profiles:read`, `sync:read` |
| **Chuyển mail hằng ngày** (khuyên dùng) | `profiles:read`, `profiles:write`, `sync:read`, `sync:write` |
| Thêm batch nhiều mailbox | thêm `batch:write` |
| Thêm đặt lịch tự động | thêm `schedules:read`, `schedules:write` |
| Quản trị luôn cả key, webhook, thông báo | `admin` (bao gồm tất cả) |

Không chắc thì chọn dòng "khuyên dùng". Muốn đổi quyền sau này: bấm **Edit**
cạnh key — key giữ nguyên, không phải cấu hình lại Claude.

> Chỉ cấp `admin` khi thật sự cần: với quyền này Claude có thể tạo thêm key và
> đọc mọi cài đặt.

---

## 3. Kết nối vào Claude

### Claude Code

```bash
claude mcp add imapsync \
  -e IMAPSYNC_URL=https://sync.wppanel.io.vn \
  -e IMAPSYNC_API_KEY=isk_xxxxxxxxxxxxxxxx \
  -- /đường/dẫn/tới/imapsync-mcp
```

Ví dụ trên máy này:
`/Users/tuananhle/Desktop/imapsync/web/mcp/imapsync-mcp`

Mặc định chỉ dùng trong thư mục hiện tại. Muốn dùng ở mọi thư mục, thêm
`--scope user` ngay sau `add`.

Kiểm tra:

```bash
claude mcp list          # imapsync phải ở trạng thái ✓ Connected
```

Mở phiên Claude Code mới (hoặc gõ `/mcp`) là có tool `imapsync`.

### Claude Desktop

Mở **Settings → Developer → Edit Config**, thêm vào
`claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "imapsync": {
      "command": "/đường/dẫn/tới/imapsync-mcp",
      "env": {
        "IMAPSYNC_URL": "https://sync.wppanel.io.vn",
        "IMAPSYNC_API_KEY": "isk_xxxxxxxxxxxxxxxx"
      }
    }
  }
}
```

Windows: đường dẫn dạng `"C:\\Tools\\imapsync-mcp-windows-amd64.exe"`.
Thoát hẳn Claude Desktop rồi mở lại.

### Kiểm tra nhanh

Hỏi Claude: **"imapsync: tôi đang đăng nhập bằng tài khoản nào?"** — Claude gọi
`whoami` và trả lời tên tài khoản. Được là đã kết nối xong.

### Biến môi trường

| Biến | Ý nghĩa |
|---|---|
| `IMAPSYNC_URL` | địa chỉ trang, có hay không có `/api` đều được |
| `IMAPSYNC_API_KEY` | key `isk_...` ở bước 2 |
| `IMAPSYNC_MCP_ALLOW_DELETE` | đặt `1` để cho phép các option **xoá mail** (xem mục 6) |

---

## 4. Sử dụng

Cứ nói bằng tiếng Việt bình thường. Claude tự chọn tool; Claude Code sẽ hỏi
xác nhận trước những việc làm thay đổi dữ liệu.

### Cách 1 — Nhập thông tin mailbox ngay trong câu lệnh

> Chuyển mail từ **a@congty-cu.com** (server **mail.congty-cu.com**, mật khẩu
> **Abc@123**) sang **a@congty-moi.com** (server **imap.congty-moi.com**, mật
> khẩu **Xyz@456**). Chạy thử trước, ổn thì chuyển thật và báo tôi khi xong.

Thông tin mỗi mailbox gồm: **server IMAP**, **tài khoản**, **mật khẩu**; thêm
nếu cần: tắt SSL, cổng khác 993. Ví dụ: *"server mail.abc.vn, cổng 143, không
SSL"*.

Hay dùng lại cặp này? Nói thêm *"lưu lại thành profile tên Công ty A"*.

### Cách 2 — Dùng profile đã lưu

Profile lưu sẵn trên server (tạo trên web hoặc nhờ Claude tạo), mật khẩu được
mã hoá và **không đi qua cuộc trò chuyện**:

> Chuyển mail profile Công ty A.

### Câu lệnh mẫu

**Chuyển mail**

| Bạn nói | Claude làm |
|---|---|
| "Có những profile nào?" | liệt kê profile |
| "Mailbox nguồn Công ty A có những folder nào, bao nhiêu mail?" | quét folder (mailbox lớn mất vài phút, Claude tự chờ) |
| "Chạy thử Công ty A" | dry run: kết nối, báo sẽ chuyển gì, **không ghi gì** |
| "Chuyển thật, chỉ INBOX và Sent" | chuyển các folder đã chọn |
| "Chỉ chuyển mail 90 ngày gần nhất" | thêm option `maxage: 90` |
| "Chờ xong rồi báo tôi" | theo dõi đến khi job kết thúc, tóm tắt kết quả |
| "Đang chạy những gì? Dừng job 42" | xem job đang chạy, dừng job |
| "Job 42 sao lỗi?" | đọc log, giải thích nguyên nhân (sai mật khẩu, timeout…) |

**Nhiều mailbox một lúc (batch)** — cần scope `batch:write`

> Chuyển các mailbox sau từ mail.cu.vn sang mail.moi.vn:
> nv1@cu.vn / pass1 → nv1@moi.vn / pass1
> nv2@cu.vn / pass2 → nv2@moi.vn / pass2
> Chạy thử trước.

Claude tự tạo CSV, chuẩn bị batch, chạy và theo dõi. Các mailbox chạy lần lượt
từng cái một.

**Đặt lịch** — cần scope `schedules:*`

> Cứ 3 giờ sáng mỗi ngày đồng bộ lại profile Công ty A.
> Tạm dừng lịch của Công ty A.

Giờ theo múi giờ server (UTC+7).

**Lịch sử, thống kê, dọn dẹp**

> Tuần này chuyển được bao nhiêu mail? Job nào lỗi?
> Xoá lịch sử các job cũ hơn 90 ngày.
> Bật tự động dọn lịch sử, giữ 60 ngày.

**Quản trị** — cần scope `admin`

> Tạo API key chỉ đọc cho bot giám sát.
> Thêm webhook gọi https://example.com/hook khi job xong.
> Gửi email thử / tin nhắn Telegram thử.

---

## 5. Hiểu kết quả

- **Trạng thái job:** `running` (đang chạy), `completed` (xong), `failed`
  (lỗi), `aborted` (bị dừng), `interrupted` (server khởi động lại giữa chừng).
- Mọi job Claude chạy đều hiện trên web ở **Running Jobs** và **History** — có
  thể theo dõi song song trên trình duyệt.
- Mail chỉ được **copy** sang đích; mail ở nguồn vẫn còn nguyên.
- Chạy lại cùng một cặp sẽ không chép trùng: mail đã có ở đích được bỏ qua.

---

## 6. An toàn

- **Không xoá mail.** Các option xoá (`delete1`, `delete2`,
  `delete2duplicates`, `expunge1`, `expunge2`) bị từ chối ở mọi đường — câu
  lệnh, profile, batch — trừ khi cấu hình `IMAPSYNC_MCP_ALLOW_DELETE=1`. Chạy
  thử (dry run) luôn được phép.
- **Mật khẩu nhập trực tiếp nằm trong cuộc trò chuyện** (gửi tới nhà cung cấp
  AI, lưu trong lịch sử chat). Nên:
  - dùng **app password** (Gmail, Microsoft 365…) và thu hồi sau khi chuyển xong;
  - với khách hàng lâu dài, lưu profile trên web rồi bảo Claude dùng profile.
- **API key là chìa khoá vào server.** Không chia sẻ, không commit lên git.
  Lộ key: vào **API Access** bấm **Revoke** hoặc **Delete**.
- Claude Code hỏi xác nhận trước khi xoá/dừng. Đừng chọn "don't ask again" cho
  các tool `delete_*`, `purge_history`, `abort_*`.

---

## 7. Xử lý sự cố

| Hiện tượng | Nguyên nhân / cách xử lý |
|---|---|
| `claude mcp list` báo Failed | sai đường dẫn binary, hoặc thiếu quyền chạy (`chmod +x`) |
| Báo "set IMAPSYNC_URL and IMAPSYNC_API_KEY" | thiếu biến môi trường trong lệnh `claude mcp add` / file config |
| `401 Invalid API key` | key sai, đã bị xoá |
| `401 ... revoked or has expired` | key bị thu hồi hoặc hết hạn → tạo key mới, cập nhật cấu hình |
| `403 This API key does not carry the "x" scope` | key thiếu quyền → **Edit** key, tick thêm scope `x` |
| `429 Rate limit exceeded` | gọi quá nhiều trong 1 phút → chờ, hoặc **Edit** key tăng rate limit |
| `refused: delete1 would delete mail` | đúng thiết kế, xem mục 6 |
| Quét folder lâu, báo `running` | bình thường với mailbox lớn; Claude gọi lại để chờ tiếp |
| Job `failed` | hỏi Claude *"đọc log job N, sao lỗi?"* — thường là sai mật khẩu, server chặn IMAP, chưa bật app password |
| `Folder listing not found` | kết quả quét đã quá 15 phút hoặc server vừa khởi động lại → quét lại |

Đổi key hoặc địa chỉ trong Claude Code:

```bash
claude mcp remove imapsync
claude mcp add imapsync -e IMAPSYNC_URL=... -e IMAPSYNC_API_KEY=isk_mới -- /đường/dẫn/imapsync-mcp
```

---

## 8. Cập nhật phiên bản

```bash
cd web/mcp
git pull
make build
./imapsync-mcp --version
```

Khởi động lại phiên Claude Code / Claude Desktop để nạp bản mới.

Tài liệu kỹ thuật (tiếng Anh): [README.md](README.md). Danh sách endpoint API:
[external-api-syncmail.md](../../external-api-syncmail.md).
