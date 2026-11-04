package local.camerahub.tv

import android.content.Context
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.view.Gravity
import android.view.View
import android.widget.Button
import android.widget.TextView

class TvStyle(val context: Context) {
    val ink = Color.rgb(17, 18, 22)
    val panel = Color.rgb(31, 33, 39)
    val edge = Color.rgb(68, 70, 79)
    val paper = Color.rgb(250, 244, 235)
    val muted = Color.rgb(187, 188, 199)
    val accent = Color.rgb(247, 169, 126)
    val accentDark = Color.rgb(71, 42, 31)
    val regular: Typeface = context.resources.getFont(R.font.ibm_plex_sans_thai_regular)
    val medium: Typeface = context.resources.getFont(R.font.ibm_plex_sans_thai_medium)
    fun dp(value: Int) = (value * context.resources.displayMetrics.density + .5f).toInt()
    fun shape(color: Int, border: Int = edge, width: Int = 1, radius: Int = 12) = GradientDrawable().apply {
        setColor(color); cornerRadius = dp(radius).toFloat(); setStroke(dp(width), border)
    }
    fun text(label: String, size: Float = 16f, color: Int = paper, bold: Boolean = false) = TextView(context).apply {
        text = label; textSize = size; setTextColor(color); typeface = if (bold) medium else regular
        includeFontPadding = false
    }
    fun button(label: String, action: () -> Unit) = Button(context).apply {
        id = View.generateViewId(); text = label; textSize = 16f; typeface = medium
        isAllCaps = false; isFocusable = true; isFocusableInTouchMode = true
        includeFontPadding = false; gravity = Gravity.CENTER; minWidth = 0; minimumWidth = 0
        minHeight = dp(48); minimumHeight = dp(48); stateListAnimator = null
        setPadding(dp(10), dp(8), dp(10), dp(8)); setTextColor(paper)
        background = shape(panel)
        setOnFocusChangeListener { _, focused ->
            background = shape(if (focused) accent else panel, if (focused) paper else edge, if (focused) 2 else 1)
            setTextColor(if (focused) ink else paper)
            elevation = if (focused) dp(5).toFloat() else 0f
        }
        setOnClickListener { action() }
    }
}
