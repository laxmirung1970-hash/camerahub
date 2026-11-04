package local.camerahub.tv

import java.net.URI

object ServerAddress {
    fun normalize(value: String): String {
        val uri = URI(value.trim())
        require(uri.scheme == "https" && !uri.host.isNullOrBlank()) { "ใช้ HTTPS จาก Tailscale Serve เช่น https://nas.example.ts.net" }
        require(uri.userInfo == null && uri.rawQuery == null && uri.fragment == null && (uri.path.isNullOrEmpty() || uri.path == "/")) { "ระบุเฉพาะที่อยู่เครื่องกลาง ไม่มีรหัสผ่านหรือเส้นทางต่อท้าย" }
        return "https://${uri.rawAuthority}".trimEnd('/')
    }
}
