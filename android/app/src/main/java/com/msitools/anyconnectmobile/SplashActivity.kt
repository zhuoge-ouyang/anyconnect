package com.msitools.anyconnectmobile

import android.app.Activity
import android.content.Intent
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.Gravity
import android.view.ViewGroup
import android.widget.FrameLayout
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.ProgressBar
import android.widget.TextView

class SplashActivity : Activity() {
    private val handler = Handler(Looper.getMainLooper())

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(createView())
        handler.postDelayed({
            startActivity(Intent(this, MainActivity::class.java))
            finish()
        }, SPLASH_MS)
    }

    override fun onDestroy() {
        handler.removeCallbacksAndMessages(null)
        super.onDestroy()
    }

    private fun createView(): FrameLayout {
        val root = FrameLayout(this)
        root.addView(ImageView(this).apply {
            setImageResource(R.drawable.splash_healing)
            scaleType = ImageView.ScaleType.CENTER_CROP
        }, FrameLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT,
            ViewGroup.LayoutParams.MATCH_PARENT,
        ))

        val density = resources.displayMetrics.density
        val panel = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER
            setPadding((24 * density).toInt(), (18 * density).toInt(), (24 * density).toInt(), (18 * density).toInt())
            background = rounded(0xEFFFF7E6.toInt(), 28f * density, 0x99FFFFFF.toInt())
        }
        panel.addView(TextView(this).apply {
            text = getString(R.string.title)
            textSize = 25f
            typeface = Typeface.create(Typeface.SERIF, Typeface.BOLD)
            setTextColor(0xFF3F5F35.toInt())
            gravity = Gravity.CENTER
        }, matchPanel())
        panel.addView(TextView(this).apply {
            text = getString(R.string.splash_loading)
            textSize = 14f
            setTextColor(0xFF6B7F55.toInt())
            gravity = Gravity.CENTER
            setPadding(0, (8 * density).toInt(), 0, (10 * density).toInt())
        }, matchPanel())
        panel.addView(ProgressBar(this, null, android.R.attr.progressBarStyleHorizontal).apply {
            isIndeterminate = true
            indeterminateTintList = android.content.res.ColorStateList.valueOf(0xFF83B777.toInt())
        }, LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT,
            (8 * density).toInt(),
        ))

        root.addView(panel, FrameLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT,
            ViewGroup.LayoutParams.WRAP_CONTENT,
            Gravity.BOTTOM,
        ).apply {
            leftMargin = (22 * density).toInt()
            rightMargin = (22 * density).toInt()
            bottomMargin = (58 * density).toInt()
        })
        return root
    }

    private fun matchPanel(): LinearLayout.LayoutParams = LinearLayout.LayoutParams(
        ViewGroup.LayoutParams.MATCH_PARENT,
        ViewGroup.LayoutParams.WRAP_CONTENT,
    )

    private fun rounded(color: Int, radius: Float, stroke: Int): GradientDrawable =
        GradientDrawable().apply {
            setColor(color)
            cornerRadius = radius
            setStroke(2, stroke)
        }

    companion object {
        private const val SPLASH_MS = 1700L
    }
}
