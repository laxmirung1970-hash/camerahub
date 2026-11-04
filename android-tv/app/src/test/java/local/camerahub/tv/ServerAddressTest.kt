package local.camerahub.tv
import org.junit.Assert.assertEquals
import org.junit.Test
class ServerAddressTest {
    @Test fun validHTTPS() { assertEquals("https://nas.example.ts.net", ServerAddress.normalize(" https://nas.example.ts.net/ ")) }
    @Test(expected = IllegalArgumentException::class) fun refusesCleartext() { ServerAddress.normalize("http://192.168.1.5") }
    @Test(expected = IllegalArgumentException::class) fun refusesCredentialURL() { ServerAddress.normalize("https://owner:secret@nas.example.ts.net") }
    @Test(expected = IllegalArgumentException::class) fun refusesQuery() { ServerAddress.normalize("https://nas.example.ts.net?token=secret") }
}
