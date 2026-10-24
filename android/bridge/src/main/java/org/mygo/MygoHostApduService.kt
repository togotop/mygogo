package org.mygo

import android.nfc.cardemulation.HostApduService
import android.os.Bundle

/**
 * 卡模拟的服务入口。
 *
 * APDU 的处理不在这里：MygoPlatform 把请求放进队列，Go 侧取出、算好应答再放回，
 * 这里只负责同步等待和兜底状态字。系统只会拉起已声明的服务，所以每个宿主都要在
 * manifest 里声明它（见 apduservice.xml 与 HOST_APDU_SERVICE 的 intent-filter）。
 */
class MygoHostApduService : HostApduService() {

    override fun processCommandApdu(commandApdu: ByteArray?, extras: Bundle?): ByteArray =
        MygoPlatform.hceApdu(commandApdu ?: ByteArray(0))

    override fun onDeactivated(reason: Int) {
        MygoPlatform.hceDeactivated(reason)
    }
}
