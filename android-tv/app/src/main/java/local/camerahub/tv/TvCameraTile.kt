package local.camerahub.tv

import android.content.Context
import android.graphics.Color
import android.graphics.drawable.GradientDrawable
import android.text.TextUtils
import android.view.Gravity
import android.view.View
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.ProgressBar
import android.widget.TextView

/** Presentation only: can be laid out and checked without camera accounts or a video decoder. */
class TvCameraTile(context: Context, val cameraId: String) : FrameLayout(context) {
    private val s = TvStyle(context)
    val videoHost = FrameLayout(context)
    private val cover = FrameLayout(context)
    private val issueTitle = s.text("กำลังเปิดภาพ", 19f, s.paper, true)
    private val issueDetail = s.text("เชื่อมต่อผ่านเครื่องกลาง", 14f, s.muted)
    private val retryHint = s.text("", 13f, s.accent)
    private val progress = ProgressBar(context).apply { indeterminateTintList = android.content.res.ColorStateList.valueOf(s.accent) }
    private val badge = s.text("รอภาพ", 12f, s.paper, true)
    private val number = s.text("", 13f, s.paper)
    private val name = s.text("", 20f, s.paper, true)
    private val model = s.text("", 14f, s.muted)
    private val labels = LinearLayout(context)
    private var single = false
    var isLive = false
        private set

    init {
        id = View.generateViewId(); isFocusable = true; isFocusableInTouchMode = true
        setPadding(s.dp(4), s.dp(4), s.dp(4), s.dp(4)); clipToOutline = true
        background = s.shape(Color.BLACK, s.edge, 1)
        addView(videoHost, LayoutParams(-1, -1))
        cover.setBackgroundColor(Color.BLACK)
        val message = LinearLayout(context).apply {
            orientation = LinearLayout.VERTICAL; gravity = Gravity.CENTER
            setPadding(s.dp(18), s.dp(12), s.dp(18), s.dp(72))
        }
        message.addView(progress, LinearLayout.LayoutParams(s.dp(20), s.dp(20)).apply { bottomMargin = s.dp(6) })
        issueTitle.gravity = Gravity.CENTER; issueDetail.gravity = Gravity.CENTER; retryHint.gravity = Gravity.CENTER
        issueDetail.maxLines = 2; issueDetail.ellipsize = TextUtils.TruncateAt.END
        retryHint.maxLines = 1; retryHint.ellipsize = TextUtils.TruncateAt.END; retryHint.visibility = View.GONE
        message.addView(issueTitle, LinearLayout.LayoutParams(-1, -2))
        message.addView(issueDetail, LinearLayout.LayoutParams(-1, -2).apply { topMargin = s.dp(3) })
        message.addView(retryHint, LinearLayout.LayoutParams(-1, -2).apply { topMargin = s.dp(2) })
        cover.addView(message, LayoutParams(-1, -1)); addView(cover, LayoutParams(-1, -1))
        badge.setPadding(s.dp(10), s.dp(5), s.dp(10), s.dp(5))
        badge.background = s.shape(0xD922242A.toInt(), 0x66565864)
        addView(badge, LayoutParams(-2, -2, Gravity.TOP or Gravity.START).apply { leftMargin = s.dp(12); topMargin = s.dp(12) })
        addView(number, LayoutParams(-2, -2, Gravity.TOP or Gravity.END).apply { rightMargin = s.dp(15); topMargin = s.dp(15) })
        labels.apply {
            orientation = LinearLayout.VERTICAL
            background = GradientDrawable(GradientDrawable.Orientation.TOP_BOTTOM, intArrayOf(0x00000000, 0xEE000000.toInt()))
            setPadding(s.dp(16), s.dp(16), s.dp(16), s.dp(12))
        }
        name.maxLines = 1; name.ellipsize = TextUtils.TruncateAt.END
        model.maxLines = 1; model.ellipsize = TextUtils.TruncateAt.END
        labels.addView(name); labels.addView(model, LinearLayout.LayoutParams(-1, -2).apply { topMargin = s.dp(2) })
        addView(labels, LayoutParams(-1, -2, Gravity.BOTTOM))
    }

    fun focusStyle(focused: Boolean) {
        background = s.shape(Color.BLACK, if (focused) s.accent else s.edge, if (focused) 4 else 1)
        elevation = if (focused) s.dp(3).toFloat() else 0f
    }
    fun update(camera: TvCamera, index: Int, single: Boolean) {
        this.single = single; name.text = camera.name
        issueTitle.textSize = if (single) 23f else 17f
        issueDetail.textSize = if (single) 17f else 13f
        retryHint.textSize = if (single) 16f else 13f
        labelStyle(isLive)
        model.text = camera.modelLabel + if (camera.group.isNotBlank()) "  ·  ${camera.group}" else ""
        number.text = (index + 1).toString().padStart(2, '0')
        contentDescription = "${camera.name}, ${camera.modelLabel}"
    }
    fun showLive(demo: Boolean) {
        isLive = true; cover.visibility = View.GONE; progress.visibility = View.GONE
        badge.visibility = View.VISIBLE; labelStyle(true)
        badge.text = if (demo) "ภาพสาธิต" else "● ภาพสด"
        badge.setTextColor(s.paper); badge.background = s.shape(0xD922242A.toInt(), s.edge)
    }
    fun showProblem(title: String, detail: String, loading: Boolean = false) {
        isLive = false; cover.visibility = View.VISIBLE
        badge.visibility = View.GONE; labelStyle(false)
        progress.visibility = if (loading) View.VISIBLE else View.GONE
        issueTitle.text = title
        val lines = detail.split('\n', limit = 2)
        issueDetail.text = lines.firstOrNull() ?: ""
        issueDetail.maxLines = if (single) 3 else if (lines.size > 1) 1 else 2
        retryHint.text = lines.getOrNull(1) ?: ""
        retryHint.visibility = if (lines.size > 1) View.VISIBLE else View.GONE
        badge.text = if (loading) "กำลังเชื่อมต่อ" else "มีปัญหา"
        badge.setTextColor(s.accent); badge.background = s.shape(s.accentDark, s.accentDark)
    }
    /** Clear LIVE immediately while giving a short network buffer a chance before blacking the video. */
    fun showBuffering() {
        isLive = false; badge.visibility = View.VISIBLE; badge.text = "กำลังรอภาพ"; badge.setTextColor(s.accent)
    }
    private fun labelStyle(live: Boolean) {
        name.textSize = if (single) 25f else if (live) 20f else 18f
        model.textSize = if (single || live) 14f else 13f
        labels.setPadding(s.dp(16), s.dp(if (live) 16 else 8), s.dp(16), s.dp(if (live) 12 else 8))
    }
}
