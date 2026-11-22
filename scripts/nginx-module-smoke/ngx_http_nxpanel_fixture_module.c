#include <ngx_config.h>
#include <ngx_core.h>
#include <ngx_http.h>

static ngx_http_module_t fixture_context;

ngx_module_t ngx_http_nxpanel_fixture_module = {
    NGX_MODULE_V1,
    &fixture_context,
    NULL,
    NGX_HTTP_MODULE,
    NULL, NULL, NULL, NULL, NULL, NULL, NULL,
    NGX_MODULE_V1_PADDING
};
