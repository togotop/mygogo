package org.mygo

import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage

/**
 * FCM 的收件入口。
 *
 * 留在 app 模块而不是 bridge 里：Firebase 是宿主的选择（国内包不带它），
 * 桥不该因此背上 14 MB 的依赖。没有 google-services.json 时它不会被注册，
 * 也就不会跑（见 manifest 里同一个 service 的注释）。
 */
class MygoFirebaseService : FirebaseMessagingService() {
    override fun onNewToken(token: String) {
        MygoPlatform.deliverPushToken(token)
    }

    override fun onMessageReceived(message: RemoteMessage) {
        val data = message.data
        MygoPlatform.onPushMessage(
            this,
            message.messageId ?: data["id"].orEmpty(),
            message.notification?.title ?: data["title"].orEmpty(),
            message.notification?.body ?: data["body"].orEmpty(),
            data,
        )
    }
}
