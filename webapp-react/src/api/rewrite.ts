import { del, get, post, put } from './client'

export interface RewriteResponse {
  content: string
  content_hash: string
  path: string
  size_bytes: number
}

export interface UpdateRewriteRequest {
  content: string
  expected_content_hash?: string
  danger_confirmed: boolean
}

// 自定义 Location 保存属于危险写操作，调用方必须先做风险确认。
export function getRewrite(siteId: string): Promise<RewriteResponse> {
  return get(`/sites/${siteId}/rewrite`)
}

export function updateRewrite(
  siteId: string,
  data: UpdateRewriteRequest
): Promise<{ content_hash: string; operation_id: string }> {
  return put(`/sites/${siteId}/rewrite`, data)
}

export interface RewriteTemplateParam {
  key: string
  label: string
  type: 'string' | 'number' | 'boolean' | 'select'
  default: unknown
  required: boolean
  options?: string[]
}

export interface RewriteTemplateItem {
  id: string
  name: string
  category: string
  description: string
  params: RewriteTemplateParam[]
  template: string
  enabled: boolean
  sort_order: number
  created_at: string
  updated_at: string
}

export interface RewriteTemplateInput {
  name: string
  category?: string
  description?: string
  params?: RewriteTemplateParam[]
  template: string
  enabled?: boolean
  sort_order?: number
}

export interface RewriteTemplateRequest {
  template_id: string
  params: Record<string, unknown>
  expected_content_hash?: string
  mode?: 'replace' | 'append'
  danger_confirmed?: boolean
}

export function getRewriteTemplates(): Promise<{ templates: RewriteTemplateItem[] }> {
  return get('/rewrite/templates')
}

export function previewRewriteTemplate(data: RewriteTemplateRequest): Promise<{ content: string }> {
  return post('/rewrite/templates/preview', data)
}

export function applyRewriteTemplate(siteId: string, data: RewriteTemplateRequest): Promise<{ content_hash: string; operation_id: string }> {
  return post(`/sites/${siteId}/rewrite/apply-template`, data)
}

export function createRewriteTemplate(data: RewriteTemplateInput): Promise<RewriteTemplateItem> {
  return post('/rewrite/templates', data)
}

export function updateRewriteTemplate(templateId: string, data: RewriteTemplateInput): Promise<RewriteTemplateItem> {
  return put(`/rewrite/templates/${templateId}`, data)
}

export function deleteRewriteTemplate(templateId: string): Promise<{ deleted: boolean }> {
  return del(`/rewrite/templates/${templateId}`)
}
