import { authStore } from '../../auth'
import { cacheAPI, memberAPI, organizationDataAPI, platformSettingsAPI, quotaAPI } from '../../api'
import { confirmAction } from '../../composables/useConfirm'
import { passwordValidationMessage } from '../../utils/password'
import { errorMessage } from '../../utils/errorMessage'

export function createSettingsOrganizationActions(deps: Record<string, any>) {
  const { show, savingMember, memberForm, showMemberModal, inviting, inviteForm, inviteLink, inviteEmailSent, showInviteModal, copyingInvite, members, invitations, load, isPlatformAdmin, platformSettingsLoading, platformSettingsError, platformSettings, platformSettingsLoaded, savingPlatformSettings, changingPassword, passwordForm, showPasswordModal, deleteForm, quota, cache, router } = deps

async function addMember() {
  if (savingMember.value) return
  const email = memberForm.value.email.trim()
  if (!email) { show('请填写成员邮箱'); return }
  const passwordError = passwordValidationMessage(memberForm.value.password, '初始密码')
  if (passwordError) { show(passwordError); return }
  savingMember.value = true
  try {
    await memberAPI.create({ ...memberForm.value, email })
    memberForm.value = { email: '', display_name: '', password: '', role: 'editor' }
    showMemberModal.value = false
    show('成员已添加')
  } catch (error) {
    show(errorMessage(error, '添加成员失败'))
    savingMember.value = false
    return
  }
  try {
    members.value = await memberAPI.list()
    await authStore.refreshOrganizations()
  } catch {
    show('成员已添加，但成员列表暂未刷新，请稍后重试')
  } finally {
    savingMember.value = false
  }
}
async function inviteMember() {
  if (inviting.value) return
  inviting.value = true
  try {
    const result = await memberAPI.invite(inviteForm.value)
    inviteLink.value = `${window.location.origin}/invite/${encodeURIComponent(result.token)}`
    inviteEmailSent.value = !!result.email_sent
    inviteForm.value = { email: '', role: 'editor', ttl_hours: 72 }
    show(result.email_sent
      ? '邀请已创建，邮件已尝试发送；也可复制链接备用'
      : '邀请已创建。邮件未配置或发送失败，请复制链接发送给成员', 4200)
  } catch (error) {
    show(errorMessage(error, '创建邀请失败'))
    inviting.value = false
    return
  }
  try {
    invitations.value = await memberAPI.invitations()
  } catch {
    show('邀请已创建，但邀请列表暂未刷新', 4200)
  } finally {
    inviting.value = false
  }
}

async function revokeInvitation(invitation: any) {
  if (!await confirmAction({
    title: '撤销邀请',
    message: `确定撤销发往 ${invitation.email} 的邀请？`,
    detail: '对方将无法再通过该邀请链接加入组织。',
    confirmText: '撤销邀请',
    tone: 'danger',
  })) return
  try {
    await memberAPI.revokeInvitation(invitation.id)
    show('邀请已撤销')
  } catch (error) {
    show(errorMessage(error, '撤销邀请失败'))
    return
  }
  try { invitations.value = await memberAPI.invitations() } catch { show('邀请已撤销，但邀请列表暂未刷新') }
}

async function resendInvitation(invitation: any) {
  if (inviting.value) return
  inviting.value = true
  try {
    const result = await memberAPI.resendInvitation(invitation.id)
    inviteLink.value = `${window.location.origin}/invite/${encodeURIComponent(result.token)}`
    inviteEmailSent.value = !!result.email_sent
    showInviteModal.value = true
    show(result.email_sent
      ? '邀请已重发，邮件已尝试发送；也可复制新链接'
      : '邀请已重发。邮件未配置或发送失败，请复制新链接', 4200)
  } catch (error) {
    show(errorMessage(error, '重发邀请失败'))
    inviting.value = false
    return
  }
  try {
    invitations.value = await memberAPI.invitations()
  } catch {
    show('邀请已重发，但邀请列表暂未刷新', 4200)
  } finally {
    inviting.value = false
  }
}

function invitationStatusLabel(status: string) {
  switch (status) {
    case 'pending': return '待接受'
    case 'accepted': return '已接受'
    case 'revoked': return '已撤销'
    case 'expired': return '已过期'
    default: return status || '未知'
  }
}

async function copyInviteLink() {
  if (!inviteLink.value || copyingInvite.value) return
  copyingInvite.value = true
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(inviteLink.value)
    } else {
      const input = document.getElementById('invite-link') as HTMLInputElement | null
      if (!input) throw new Error('invite link input missing')
      input.focus()
      input.select()
      document.execCommand('copy')
    }
    show('邀请链接已复制到剪贴板')
  } catch {
    show('复制失败，请手动选中链接复制', 3600)
  } finally {
    copyingInvite.value = false
  }
}

function openInviteModal() {
  inviteLink.value = ''
  inviteEmailSent.value = null
  inviteForm.value = { email: '', role: 'editor', ttl_hours: 72 }
  showInviteModal.value = true
}

function closeInviteModal() {
  if (inviting.value) return
  showInviteModal.value = false
  inviteLink.value = ''
  inviteEmailSent.value = null
}

async function changeMemberRole(member: any) {
  try {
    await memberAPI.update(member.user_id, member.role)
    show('角色已更新')
  } catch (error) {
    show(errorMessage(error, '更新角色失败'))
    await load()
  }
}

async function removeMember(member: any) {
  if (!await confirmAction({
    title: '移除成员',
    message: `确定将 ${member.email} 移出当前组织？`,
    detail: '该成员将立即失去组织内全部数据的访问权限。',
    confirmText: '移除成员',
    tone: 'danger',
  })) return
  try {
    await memberAPI.remove(member.user_id)
    show('成员已移除')
  } catch (error) {
    show(errorMessage(error, '移除成员失败'))
    return
  }
  try { members.value = await memberAPI.list() } catch { show('成员已移除，但成员列表暂未刷新') }
}

async function loadPlatformSettings() {
  if (!isPlatformAdmin.value || platformSettingsLoading.value) return
  platformSettingsLoading.value = true
  platformSettingsError.value = ''
  try {
    const data = await platformSettingsAPI.get()
    platformSettings.value = {
      registration_enabled: data.registration_enabled !== false,
      require_email_verification: !!data.require_email_verification,
    }
    platformSettingsLoaded.value = true
  } catch (error) {
    platformSettingsError.value = errorMessage(error, '加载注册设置失败')
  } finally {
    platformSettingsLoading.value = false
  }
}

async function savePlatformSettings() {
  if (!isPlatformAdmin.value || savingPlatformSettings.value) return
  savingPlatformSettings.value = true
  platformSettingsError.value = ''
  try {
    const data = await platformSettingsAPI.update({
      registration_enabled: !!platformSettings.value.registration_enabled,
      require_email_verification: !!platformSettings.value.require_email_verification,
    })
    platformSettings.value = {
      registration_enabled: data.registration_enabled !== false,
      require_email_verification: !!data.require_email_verification,
    }
    platformSettingsLoaded.value = true
    show('注册设置已保存')
  } catch (error) {
    platformSettingsError.value = errorMessage(error, '保存注册设置失败')
    show(platformSettingsError.value)
  } finally {
    savingPlatformSettings.value = false
  }
}

async function changePassword() {
  if (changingPassword.value) return
  if (!passwordForm.value.current) { show('请填写当前密码'); return }
  if (passwordForm.value.next !== passwordForm.value.confirm) { show('两次新密码不一致'); return }
  const passwordError = passwordValidationMessage(passwordForm.value.next, '新密码')
  if (passwordError) { show(passwordError); return }
  changingPassword.value = true
  try {
    await authStore.changePassword(passwordForm.value.current, passwordForm.value.next)
    passwordForm.value = { current: '', next: '', confirm: '' }
    showPasswordModal.value = false
    show('密码已更新')
  } catch (error) {
    show(errorMessage(error, '修改密码失败'))
  } finally {
    changingPassword.value = false
  }
}

async function exportOrganization() {
  try {
    const data = await organizationDataAPI.export()
    const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = `${authStore.state.actor?.organization.slug || 'organization'}-export.json`
    anchor.click()
    URL.revokeObjectURL(url)
    show('组织数据已导出')
  } catch (error) {
    show(errorMessage(error, '导出失败'))
  }
}

async function deleteOrganization() {
  if (!await confirmAction({
    title: '永久删除组织',
    message: '确定永久删除组织及其全部数据？此操作无法撤销。',
    detail: '组织下的所有项目、剧集、素材与成员关系都会被清除，删除后将自动退出登录。',
    confirmText: '确认永久删除',
    tone: 'danger',
  })) return
  try {
    await organizationDataAPI.remove(deleteForm.value.password, deleteForm.value.confirmation)
    await authStore.logout()
    await router.replace('/login')
  } catch (error) {
    show(errorMessage(error, '删除组织失败'))
  }
}

async function saveQuota() {
  try {
    await quotaAPI.update({ daily_job_limit: quota.value.daily_job_limit, max_active_jobs: quota.value.max_active_jobs, daily_budget_cny: quota.value.daily_budget_cny, budget_warning_percent: quota.value.budget_warning_percent })
    show('生成配额已保存')
  } catch (error) {
    show(errorMessage(error, '保存配额失败'))
    return
  }
  try {
    quota.value = await quotaAPI.get()
  } catch {
    show('生成配额已保存，但最新用量暂未刷新')
  }
}

async function purgeCache() {
  if (!await confirmAction({
    title: '清理过期缓存',
    message: '确定清理当前组织的过期缓存？',
    detail: '仅清理已过期的缓存条目，不影响项目数据。',
    confirmText: '清理缓存',
  })) return
  try {
    await cacheAPI.purge()
    cache.value = await cacheAPI.stats()
    show('过期缓存已清理')
  } catch (error) {
    show(errorMessage(error, '清理缓存失败'))
  }
}

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`
  return `${(value / 1024 / 1024).toFixed(1)} MB`
}

  return { addMember, inviteMember, revokeInvitation, resendInvitation, invitationStatusLabel, copyInviteLink, openInviteModal, closeInviteModal, changeMemberRole, removeMember, loadPlatformSettings, savePlatformSettings, changePassword, exportOrganization, deleteOrganization, saveQuota, purgeCache, formatBytes }
}
