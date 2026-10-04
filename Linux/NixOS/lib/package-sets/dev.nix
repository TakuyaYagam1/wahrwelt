{ pkgs }:
let
  buildTools = with pkgs; [
    cmake
    ninja
    gnumake
    pkg-config
    tree-sitter
    just
    gcc
    libgcc
    clang
    clang-tools
    lld
    gdb
    musl
  ];
  buildExtraTools = with pkgs; [
    templ
    mold
    lldb
    valgrind
    cppcheck
    bear
    meson
    conan
    vcpkg
  ];
  pythonTools = with pkgs; [
    python3
    pipx
    uv
    ruff
    mypy
  ];
  goTools = with pkgs; [
    go
    gopls
    delve
    golangci-lint
    gofumpt
    govulncheck
    grpcurl
    vips
  ];
  goExtraTools = with pkgs; [
    air
    mockgen
    go-swag
    oapi-codegen
    ogen
    go-mockery
    go-migrate
    protobuf
    protoc-gen-go
    protoc-gen-go-grpc
    buf
    sqlc
  ];
  jsTools = with pkgs; [
    nodejs
    corepack
    pnpm
    typescript
    typescript-language-server
    vscode-langservers-extracted
    yaml-language-server
  ];
  jsExtraTools = with pkgs; [
    yarn
    tailwindcss-language-server
    bun
    deno
    eslint_d
    prettierd
    biome
    playwright
  ];
  rustTools = with pkgs; [
    rustc
    cargo
    rustfmt
    clippy
    rust-analyzer
    cargo-nextest
  ];
  rustExtraTools = with pkgs; [
    cargo-edit
    cargo-watch
    cargo-audit
    cargo-deny
    cargo-expand
    sccache
  ];
  jvmTools = with pkgs; [
    jdk
    maven
    gradle
    jdt-language-server
    google-java-format
    checkstyle
    pmd
    liquibase
  ];
  rubyTools =
    with pkgs;
    [
      ruby
      bundler
      rubocop
      ruby-lsp
      solargraph
      rubyfmt
      brakeman
      bundler-audit
    ]
    ++ (with pkgs.rubyPackages; [
      pry
      rspec
      standard
    ]);
  iacTools = with pkgs; [
    terraform
  ];
  iacExtraTools = with pkgs; [
    tflint
    terraform-docs
    ansible
  ];
  containerTools = with pkgs; [
    lazydocker
    kubectl
  ];
  containerExtraTools = with pkgs; [
    k9s
    kind
    kubernetes-helm
    kustomize
    tilt
    cilium-cli
    cosign
    cri-tools
    eksctl
    spire
  ];
  generalTools = with pkgs; [
    httpie
    yq-go
    lazygit
    tldr
    gh
  ];
  generalExtraTools = with pkgs; [
    mercurial
    subversion
    opencv
    typst
    ghgrab
  ];
in
{
  devTools =
    buildTools
    ++ pythonTools
    ++ goTools
    ++ jsTools
    ++ rustTools
    ++ iacTools
    ++ containerTools
    ++ generalTools;
  personalDevTools =
    buildExtraTools
    ++ goExtraTools
    ++ jsExtraTools
    ++ rustExtraTools
    ++ jvmTools
    ++ rubyTools
    ++ iacExtraTools
    ++ containerExtraTools
    ++ generalExtraTools;
}
