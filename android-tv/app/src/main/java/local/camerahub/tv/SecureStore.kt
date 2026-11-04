package local.camerahub.tv

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

class SecureStore(context: Context) {
    private val prefs = context.getSharedPreferences("camerahub", Context.MODE_PRIVATE)
    private fun key(): SecretKey {
        val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (ks.getKey("camerahub-token", null) as? SecretKey)?.let { return it }
        return KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore").apply {
            init(KeyGenParameterSpec.Builder("camerahub-token", KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM).setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).build())
        }.generateKey()
    }
    var server: String
        get() = prefs.getString("server", "") ?: ""
        set(value) { prefs.edit().putString("server", value).apply() }
    fun saveConnection(server: String, token: String) {
        val cipher = Cipher.getInstance("AES/GCM/NoPadding").apply { init(Cipher.ENCRYPT_MODE, key()) }
        val body = cipher.doFinal(token.toByteArray(Charsets.UTF_8))
        check(prefs.edit().putString("server", server)
            .putString("token", Base64.encodeToString(cipher.iv + body, Base64.NO_WRAP)).commit()) { "Cannot save TV connection" }
    }
    fun token(): String = try {
        val raw = Base64.decode(prefs.getString("token", "") ?: "", Base64.NO_WRAP)
        if (raw.size < 28) "" else {
            val cipher = Cipher.getInstance("AES/GCM/NoPadding").apply { init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, raw.copyOfRange(0, 12))) }
            String(cipher.doFinal(raw.copyOfRange(12, raw.size)), Charsets.UTF_8)
        }
    } catch (_: Exception) { "" }
    fun clear() { prefs.edit().remove("token").apply() }
}
