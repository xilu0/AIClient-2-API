这里提供的api key有限制，只能用于claude code。主要关注开发效果，稳定性。
内测api对应的大模型来自新的供应商或新的claude渠道。
key1:
export ANTHROPIC_BASE_URL="https://claude-code.club/api"
export ANTHROPIC_AUTH_TOKEN="cr_3e9acdf4cdd0f678edc93c9b8d6172d00a2345f1018d03920d2f3784422ffec9"


第一步：打开编辑器和终端，推荐使用vscode内的终端。
第二步：在终端中输入命令
注意：这个是临时将环境变量修改，不会更改你原来的环境变量。这个临时更改只在当前终端中生效，关闭当前终端将会失效；
macos设置临时变量：
  export ANTHROPIC_BASE_URL="https://claude-code.club/api"
  export ANTHROPIC_AUTH_TOKEN="cr_3e9acdf4cdd0f678edc93c9b8d6172d00a2345f1018d03920d2f3784422ffec9"
windows powershell设置临时变量：
$env:ANTHROPIC_BASE_URL="https://claude-code.club/api"
$env:ANTHROPIC_AUTH_TOKEN="cr_3e9acdf4cdd0f678edc93c9b8d6172d00a2345f1018d03920d2f3784422ffec9"
第三步：在当前终端中启动claude，直接体验。
关注话费执行 /cost 命令。