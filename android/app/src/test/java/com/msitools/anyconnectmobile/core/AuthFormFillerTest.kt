package com.msitools.anyconnectmobile.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AuthFormFillerTest {
    @Test
    fun fillsUsernameAndPasswordFieldsOnly() {
        val form = MutableAuthForm(
            options = listOf(
                MutableAuthOption(name = "username", label = "Username", type = AuthOptionType.TEXT),
                MutableAuthOption(name = "password", label = "Password", type = AuthOptionType.PASSWORD),
            ),
        )

        val result = AuthFormFiller("alice", "secret").fill(form)

        assertEquals(AuthFillResult.OK, result)
        assertEquals("alice", form.options[0].value)
        assertEquals("secret", form.options[1].value)
    }

    @Test
    fun rejectsUnsupportedRequiredSelectFields() {
        val form = MutableAuthForm(
            options = listOf(
                MutableAuthOption(name = "group", label = "Group", type = AuthOptionType.SELECT),
            ),
        )

        val result = AuthFormFiller("alice", "secret").fill(form)

        assertTrue(result is AuthFillResult.Unsupported)
        assertEquals("group", (result as AuthFillResult.Unsupported).fieldName)
    }

    @Test
    fun ignoresHiddenFieldsAndIgnoredFields() {
        val form = MutableAuthForm(
            options = listOf(
                MutableAuthOption(name = "hidden", label = "Hidden", type = AuthOptionType.HIDDEN),
                MutableAuthOption(name = "ignored", label = "Ignored", type = AuthOptionType.TEXT, ignored = true),
                MutableAuthOption(name = "user", label = "User", type = AuthOptionType.TEXT),
                MutableAuthOption(name = "pass", label = "Pass", type = AuthOptionType.PASSWORD),
            ),
        )

        val result = AuthFormFiller("alice", "secret").fill(form)

        assertEquals(AuthFillResult.OK, result)
        assertEquals(null, form.options[0].value)
        assertEquals(null, form.options[1].value)
        assertEquals("alice", form.options[2].value)
        assertEquals("secret", form.options[3].value)
    }

    @Test
    fun acceptsUsernameOnlyFirstStepForms() {
        val form = MutableAuthForm(
            options = listOf(
                MutableAuthOption(name = "username", label = "Username", type = AuthOptionType.TEXT),
            ),
        )

        val result = AuthFormFiller("alice", "secret").fill(form)

        assertEquals(AuthFillResult.OK, result)
        assertEquals("alice", form.options[0].value)
    }

    @Test
    fun acceptsPasswordOnlySecondStepForms() {
        val form = MutableAuthForm(
            options = listOf(
                MutableAuthOption(name = "password", label = "Password", type = AuthOptionType.PASSWORD),
            ),
        )

        val result = AuthFormFiller("alice", "secret").fill(form)

        assertEquals(AuthFillResult.OK, result)
        assertEquals("secret", form.options[0].value)
    }
}
