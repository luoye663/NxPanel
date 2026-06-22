import { Button, Group } from '@mantine/core'
import CodeMirror, { EditorView } from '@uiw/react-codemirror'
import { StreamLanguage, indentUnit } from '@codemirror/language'
import type { StreamParser } from '@codemirror/language'
import { c, cpp, java } from '@codemirror/legacy-modes/mode/clike'
import { css } from '@codemirror/legacy-modes/mode/css'
import { dockerFile } from '@codemirror/legacy-modes/mode/dockerfile'
import { go } from '@codemirror/legacy-modes/mode/go'
import { javascript, json, typescript } from '@codemirror/legacy-modes/mode/javascript'
import { lua } from '@codemirror/legacy-modes/mode/lua'
import { perl } from '@codemirror/legacy-modes/mode/perl'
import { properties } from '@codemirror/legacy-modes/mode/properties'
import { python } from '@codemirror/legacy-modes/mode/python'
import { ruby } from '@codemirror/legacy-modes/mode/ruby'
import { rust } from '@codemirror/legacy-modes/mode/rust'
import { shell } from '@codemirror/legacy-modes/mode/shell'
import { standardSQL } from '@codemirror/legacy-modes/mode/sql'
import { toml } from '@codemirror/legacy-modes/mode/toml'
import { html } from '@codemirror/legacy-modes/mode/xml'
import { xml } from '@codemirror/legacy-modes/mode/xml'
import { yaml } from '@codemirror/legacy-modes/mode/yaml'
import { useEffect, useRef, useState } from 'react'
import type { CodeEditorLanguage } from '@/utils/fileType'
import { notifyError, notifySuccess, notifyWarning } from '@/utils/notify'

interface NginxCodeEditorProps {
  value: string
  onChange: (value: string) => void
  formattable?: boolean
  language?: CodeEditorLanguage
}

function words(value: string): Record<string, boolean> {
  return value.split(' ').reduce<Record<string, boolean>>((result, word) => {
    result[word] = true
    return result
  }, {})
}

type NginxStream = {
  current: () => string
  eat: (match: string | RegExp) => string | undefined | void
  eatSpace: () => boolean
  eatWhile: (match: RegExp) => boolean
  match: (pattern: RegExp) => RegExpMatchArray | boolean | null
  next: () => string | undefined | null | void
  skipToEnd: () => void
}

type NginxState = {
  tokenize: (stream: NginxStream, state: NginxState) => string | null
  baseIndent: number
  stack: string[]
}

function createPatchedNginxParser(): StreamParser<NginxState> {
  const keywords = words(
    'break return rewrite set'
      + ' accept_mutex accept_mutex_delay access_log add_after_body add_before_body add_header addition_types aio alias allow ancient_browser ancient_browser_value auth_basic auth_basic_user_file auth_http auth_http_header auth_http_timeout autoindex autoindex_exact_size autoindex_localtime charset charset_types client_body_buffer_size client_body_in_file_only client_body_in_single_buffer client_body_temp_path client_body_timeout client_header_buffer_size client_header_timeout client_max_body_size connection_pool_size create_full_put_path daemon dav_access dav_methods debug_connection debug_points default_type degradation degrade deny devpoll_changes devpoll_events directio directio_alignment empty_gif env epoll_events error_log eventport_events expires fastcgi_bind fastcgi_buffer_size fastcgi_buffers fastcgi_busy_buffers_size fastcgi_cache fastcgi_cache_key fastcgi_cache_methods fastcgi_cache_min_uses fastcgi_cache_path fastcgi_cache_use_stale fastcgi_cache_valid fastcgi_catch_stderr fastcgi_connect_timeout fastcgi_hide_header fastcgi_ignore_client_abort fastcgi_ignore_headers fastcgi_index fastcgi_intercept_errors fastcgi_max_temp_file_size fastcgi_next_upstream fastcgi_param fastcgi_pass_header fastcgi_pass_request_body fastcgi_pass_request_headers fastcgi_read_timeout fastcgi_send_lowat fastcgi_send_timeout fastcgi_split_path_info fastcgi_store fastcgi_store_access fastcgi_temp_file_write_size fastcgi_temp_path fastcgi_upstream_fail_timeout fastcgi_upstream_max_fails flv geoip_city geoip_country google_perftools_profiles gzip gzip_buffers gzip_comp_level gzip_disable gzip_hash gzip_http_version gzip_min_length gzip_no_buffer gzip_proxied gzip_static gzip_types gzip_vary gzip_window if_modified_since ignore_invalid_headers image_filter image_filter_buffer image_filter_jpeg_quality image_filter_transparency imap_auth imap_capabilities imap_client_buffer index ip_hash keepalive_requests keepalive_timeout kqueue_changes kqueue_events large_client_header_buffers limit_conn limit_conn_log_level limit_rate limit_rate_after limit_req limit_req_log_level limit_req_zone limit_zone lingering_time lingering_timeout lock_file log_format log_not_found log_subrequest map_hash_bucket_size map_hash_max_size master_process memcached_bind memcached_buffer_size memcached_connect_timeout memcached_next_upstream memcached_read_timeout memcached_send_timeout memcached_socket_keepalive merge_slashes min_delete_depth modern_browser modern_browser_value msie_padding msie_refresh multi_accept open_file_cache open_file_cache_errors open_file_cache_events open_file_cache_min_uses open_file_cache_valid open_log_file_cache output_buffers override_charset perl perl_modules perl_require perl_set pid pop3_auth pop3_capabilities port_in_redirect postpone_gzipping postpone_output protocol proxy proxy_bind proxy_buffer proxy_buffer_size proxy_buffering proxy_buffers proxy_busy_buffers_size proxy_cache_bypass proxy_cache_key proxy_cache_lock proxy_cache_methods proxy_cache_min_uses proxy_cache_path proxy_cache_use_stale proxy_cache_valid proxy_connect_timeout proxy_headers_hash_bucket_size proxy_headers_hash_max_size proxy_hide_header proxy_ignore_client_abort proxy_ignore_headers proxy_intercept_errors proxy_max_temp_file_size proxy_method proxy_next_upstream proxy_no_cache proxy_pass_header proxy_pass_request_body proxy_pass_request_headers proxy_read_timeout proxy_redirect proxy_send_lowat proxy_send_timeout proxy_set_body proxy_set_header proxy_ssl_session_reuse proxy_store proxy_store_access proxy_temp_file_write_size proxy_temp_path push rds_json_buffer_size rds_json_format rds_json_root random_index read_ahead recursive_error_pages request_pool_size reset_timedout_connection resolver resolver_timeout rewrite_log satisfy secure_link_secret send_lowat send_timeout sendfile sendfile_max_chunk server_name_in_redirect server_names_hash_bucket_size server_names_hash_max_size server_tokens set_real_ip_from smtp_auth smtp_capabilities smtp_client_buffer smtp_greeting_delay so_keepalive source_charset ssi ssi_ignore_recycled_buffers ssi_min_file_chunk ssi_silent_errors ssi_types ssi_value_length ssl ssl_certificate ssl_certificate_key ssl_ciphers ssl_client_certificate ssl_crl ssl_dhparam ssl_ecdh_curve ssl_engine ssl_password_file ssl_prefer_server_ciphers ssl_protocols ssl_session_cache ssl_session_timeout ssl_stapling ssl_stapling_file ssl_stapling_responder ssl_stapling_verify ssl_trusted_certificate ssl_verify_client ssl_verify_depth sub_filter sub_filter_once tcp_nodelay tcp_nopush thread_stack_size timeout timer_resolution types_hash_bucket_size types_hash_max_size underscores_in_headers uninitialized_variable_warn use user userid userid_domain userid_expires userid_mark userid_name userid_p3p userid_path userid_service valid_referers variables_hash_bucket_size variables_hash_max_size worker_aio_requests worker_connections worker_cpu_affinity worker_priority worker_processes worker_rlimit_core worker_rlimit_nofile worker_rlimit_sigpending worker_threads working_directory xclient xml_entities xslt_stylesheet xslt_types'
  )
  const keywordsBlock = words('http mail events server types location upstream charset_map limit_except if geo map')
  const keywordsImportant = words('include root server server_name listen internal proxy_pass memcached_pass fastcgi_pass try_files')

  let tokenType: string | null = null

  function ret(style: string | null, nextTokenType: string | null) {
    tokenType = nextTokenType
    return style
  }

  function tokenString(quote: string) {
    return function tokenizeString(stream: NginxStream, state: NginxState) {
      let escaped = false
      let ch: string | undefined | null | void
      while ((ch = stream.next()) != null) {
        if (ch === quote && !escaped) break
        escaped = !escaped && ch === '\\'
      }
      if (!escaped) state.tokenize = tokenBase
      return ret('string', 'string')
    }
  }

  function tokenSGMLComment(stream: NginxStream, state: NginxState) {
    let dashes = 0
    let ch: string | undefined | null | void
    while ((ch = stream.next()) != null) {
      if (dashes >= 2 && ch === '>') {
        state.tokenize = tokenBase
        break
      }
      dashes = ch === '-' ? dashes + 1 : 0
    }
    return ret('comment', 'comment')
  }

  function tokenBase(stream: NginxStream, state: NginxState) {
    stream.eatWhile(/[\w$_]/)
    const current = stream.current()

    if (Object.prototype.propertyIsEnumerable.call(keywords, current)) return 'keyword'
    if (Object.prototype.propertyIsEnumerable.call(keywordsBlock, current)) return 'controlKeyword'
    if (Object.prototype.propertyIsEnumerable.call(keywordsImportant, current)) return 'controlKeyword'

    const ch = stream.next()
    if (ch === '@') {
      stream.eatWhile(/[\w\\-]/)
      return ret('meta', stream.current())
    }
    if (ch === '<' && stream.eat('!')) {
      state.tokenize = tokenSGMLComment
      return tokenSGMLComment(stream, state)
    }
    if (ch === '=') return ret(null, 'compare')
    if ((ch === '~' || ch === '|') && stream.eat('=')) return ret(null, 'compare')
    if (ch === '"' || ch === "'") {
      state.tokenize = tokenString(ch)
      return state.tokenize(stream, state)
    }
    if (ch === '#') {
      stream.skipToEnd()
      return ret('comment', 'comment')
    }
    if (ch === '!') {
      stream.match(/^\s*\w*/)
      return ret('keyword', 'important')
    }
    if (ch != null && /\d/.test(ch)) {
      stream.eatWhile(/[\w.%]/)
      return ret('number', 'unit')
    }
    if (ch != null && /[,.+>*\/]/.test(ch)) return ret(null, 'select-op')
    if (ch != null && /[;{}:\[\]]/.test(ch)) return ret(null, ch)

    stream.eatWhile(/[\w\\-]/)
    return ret('variable', 'variable')
  }

  return {
    name: 'nginx',
    startState() {
      return {
        tokenize: tokenBase,
        baseIndent: 0,
        stack: [],
      }
    },
    token(stream, state) {
      if (stream.eatSpace()) return null
      tokenType = null
      let style = state.tokenize(stream, state)

      const context = state.stack[state.stack.length - 1]
      if (tokenType === 'hash' && context === 'rule') style = 'atom'
      else if (style === 'variable') {
        if (context === 'rule') style = 'number'
        else if (!context || context === '@media{') style = 'tag'
      }

      if (context === 'rule' && tokenType != null && /^[\{};]$/.test(tokenType)) state.stack.pop()
      if (tokenType === '{') {
        if (context === '@media') state.stack[state.stack.length - 1] = '@media{'
        else state.stack.push('{')
      } else if (tokenType === '}') state.stack.pop()
      else if (tokenType === '@media') state.stack.push('@media')
      else if (context === '{' && tokenType !== 'comment') state.stack.push('rule')

      return style
    },
    indent(state, textAfter, context) {
      let depth = state.stack.length
      if (/^\}/.test(textAfter)) depth -= state.stack[state.stack.length - 1] === 'rule' ? 2 : 1
      return state.baseIndent + depth * context.unit
    },
    languageData: {
      indentOnInput: /^\s*\}$/,
    },
  }
}

const patchedNginxParser = createPatchedNginxParser()

const languageParsers: Partial<Record<CodeEditorLanguage, StreamParser<unknown>>> = {
  nginx: patchedNginxParser,
  html,
  css,
  javascript,
  typescript,
  json,
  xml,
  yaml,
  toml,
  properties,
  shell,
  python,
  go,
  rust,
  java,
  c,
  cpp,
  sql: standardSQL,
  lua,
  perl,
  ruby,
  dockerfile: dockerFile,
}

const editorTheme = EditorView.theme({
  '&': { height: '100%' },
  '.cm-scroller': {
    overflowX: 'auto',
    overflowY: 'scroll',
    scrollbarGutter: 'stable',
  },
  '.cm-content': {
    minHeight: '100%',
    tabSize: 4,
  },
})

const NGINX_INDENT = '    '

function splitNginxStatements(value: string): string[] {
  const lines: string[] = []
  let current = ''
  let quote = ''
  let escaped = false
  let inComment = false

  for (let i = 0; i < value.length; i++) {
    const ch = value[i]
    if (inComment) {
      if (ch === '\n') {
        lines.push(current.trimEnd())
        current = ''
        inComment = false
      } else {
        current += ch
      }
      continue
    }

    if (quote) {
      current += ch
      if (ch === quote && !escaped) quote = ''
      escaped = !escaped && ch === '\\'
      continue
    }

    if (ch === '"' || ch === "'") {
      quote = ch
      current += ch
      escaped = false
      continue
    }

    if (ch === '#') {
      inComment = true
      current += ch
      continue
    }

    if (ch === '\n') {
      lines.push(current.trimEnd())
      current = ''
      continue
    }

    if (ch === '{' || ch === '}' || ch === ';') {
      current += ch
      const restOfLine = value.slice(i + 1).split('\n', 1)[0]
      if (restOfLine.trimStart().startsWith('#')) {
        current += restOfLine
        i += restOfLine.length
      }
      lines.push(current.trim())
      current = ''
      continue
    }

    current += ch
  }

  if (current.length > 0) lines.push(current.trimEnd())
  return lines
}

function formatNginxConf(value: string): string {
  const formatted: string[] = []
  let indent = 0
  let previousWasMarkerEnd = false

  function ensureBlankLine() {
    if (formatted.length > 0 && formatted[formatted.length - 1] !== '') formatted.push('')
  }

  for (const rawLine of splitNginxStatements(value)) {
    const line = rawLine.trim()
    if (!line) continue
    const isComment = line.startsWith('#')
    const markerStart = /^#\s*NXPANEL-[A-Z0-9-]+-START\b/.test(line)
    const markerEnd = /^#\s*NXPANEL-[A-Z0-9-]+-END\b/.test(line)
    if (previousWasMarkerEnd || markerStart) ensureBlankLine()
    if (!isComment && line.startsWith('}')) indent = Math.max(0, indent - 1)
    formatted.push(`${NGINX_INDENT.repeat(indent)}${line}`)
    previousWasMarkerEnd = markerEnd
    if (!isComment && line.endsWith('{')) indent += 1
  }

  return `${formatted.join('\n').trimEnd()}\n`
}

export default function NginxCodeEditor({ value, onChange, formattable = false, language = 'nginx' }: NginxCodeEditorProps) {
  const editorShellRef = useRef<HTMLDivElement>(null)
  const [editorHeight, setEditorHeight] = useState(400)
  const parser = languageParsers[language]

  useEffect(() => {
    const shell = editorShellRef.current
    if (!shell) return

    function syncEditorMetrics() {
      const gutter = shell?.querySelector<HTMLElement>('.cm-gutters')
      const width = gutter?.getBoundingClientRect().width || 0
      const height = shell?.getBoundingClientRect().height || 0
      shell?.style.setProperty('--nx-editor-gutter-width', `${Math.ceil(width)}px`)
      if (height > 0) setEditorHeight(Math.max(120, Math.floor(height)))
    }

    syncEditorMetrics()
    const frame = window.requestAnimationFrame(syncEditorMetrics)

    const gutter = shell.querySelector<HTMLElement>('.cm-gutters')
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(syncEditorMetrics)
    observer?.observe(shell)
    if (gutter) observer?.observe(gutter)

    return () => {
      window.cancelAnimationFrame(frame)
      observer?.disconnect()
    }
  }, [value])

  function handleFormat() {
    try {
      // 与 Vue 版保持一致：只格式化 Nginx 语法，不改变后端保存和 nginx -t 校验流程。
      const formatted = formatNginxConf(value)
      if (formatted === value) {
        notifyWarning({ message: '内容已是格式化状态' })
        return
      }
      onChange(formatted)
      notifySuccess({ message: '格式化完成' })
    } catch (error) {
      notifyError({ message: error instanceof Error ? error.message : '格式化失败，请检查语法' })
    }
  }

  return (
    <div className="nginxCodeEditor">
      {formattable ? (
        <Group className="nginxCodeEditorToolbar" justify="flex-end">
          <Button size="compact-xs" variant="subtle" onClick={handleFormat}>格式化</Button>
        </Group>
      ) : null}
      <div ref={editorShellRef} className="nginxCodeMirrorShell">
        <CodeMirror
          value={value}
          height={`${editorHeight}px`}
          basicSetup={{ foldGutter: true, lineNumbers: true }}
          extensions={[...(parser ? [StreamLanguage.define(parser)] : []), indentUnit.of(NGINX_INDENT), EditorView.lineWrapping, editorTheme]}
          onChange={onChange}
        />
      </div>
    </div>
  )
}
