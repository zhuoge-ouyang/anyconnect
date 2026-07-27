package com.msitools.anyconnectmobile.core

enum class AuthOptionType {
    TEXT,
    PASSWORD,
    SELECT,
    HIDDEN,
    TOKEN,
}

data class MutableAuthOption(
    val name: String,
    val label: String?,
    val type: AuthOptionType,
    val ignored: Boolean = false,
    var value: String? = null,
)

data class MutableAuthForm(
    val options: List<MutableAuthOption>,
)

sealed class AuthFillResult {
    data object OK : AuthFillResult()
    data class Unsupported(val fieldName: String, val fieldType: AuthOptionType) : AuthFillResult()
}

class AuthFormFiller(
    private val username: String,
    private val password: String,
) {
    fun fill(form: MutableAuthForm): AuthFillResult {
        var usernameFilled = false
        var passwordFilled = false

        for (option in form.options) {
            if (option.ignored || option.type == AuthOptionType.HIDDEN) continue
            when (option.type) {
                AuthOptionType.TEXT -> {
                    option.value = username
                    usernameFilled = true
                }
                AuthOptionType.PASSWORD -> {
                    option.value = password
                    passwordFilled = true
                }
                AuthOptionType.SELECT,
                AuthOptionType.TOKEN -> return AuthFillResult.Unsupported(option.name, option.type)
                AuthOptionType.HIDDEN -> Unit
            }
        }

        return AuthFillResult.OK
    }
}
