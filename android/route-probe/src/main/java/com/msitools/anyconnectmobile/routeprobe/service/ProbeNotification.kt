package com.msitools.anyconnectmobile.routeprobe.service

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.pm.ServiceInfo
import android.os.Build
import com.msitools.anyconnectmobile.routeprobe.R

object ProbeNotification {
    fun startForeground(service: Service) {
        val manager = service.getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(
            NotificationChannel(
                CHANNEL_ID,
                "Android VPN 路由容量测试",
                NotificationManager.IMPORTANCE_LOW,
            ),
        )
        val notification = Notification.Builder(service, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_probe_notification)
            .setContentTitle("正在测试 Android VPN 路由容量")
            .setCategory(Notification.CATEGORY_SERVICE)
            .setOngoing(true)
            .setAutoCancel(false)
            .build()
        if (Build.VERSION.SDK_INT >= 34) {
            service.startForeground(
                NOTIFICATION_ID,
                notification,
                ServiceInfo.FOREGROUND_SERVICE_TYPE_SYSTEM_EXEMPTED,
            )
        } else {
            service.startForeground(NOTIFICATION_ID, notification)
        }
    }

    private const val CHANNEL_ID = "route_probe"
    private const val NOTIFICATION_ID = 2_520
}
