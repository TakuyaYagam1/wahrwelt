{
  lib,
  buildNpmPackage,
  fetchFromGitHub,
  fetchurl,
  python3,
  pkg-config,
  libsecret,
  libx11,
  stdenv,
  nodejs_22,
  python311,
  vips,
  gnumake,
  gcc,
  perl,
}:

let
  nodejs = nodejs_22;
  buildNpmPackage' = buildNpmPackage.override { inherit nodejs; };
  swcBinding = fetchurl {
    url = "https://registry.npmjs.org/@swc/core-linux-x64-gnu/-/core-linux-x64-gnu-1.16.20.tgz";
    hash = "sha256-rnD1tTpShE9ihP80zgnY8xEgYHha1hg4pkj0JAOeu2o=";
  };
in
buildNpmPackage' rec {
  pname = "omnirouter";
  version = "3.8.52";

  src = fetchFromGitHub {
    owner = "diegosouzapw";
    repo = "OmniRoute";
    rev = "v3.8.52";
    hash = "sha256-k4+iy6aFWaFxVPcTStR4xNCJjw/UHeMqb5XnEvuqJZU=";
  };

  npmDepsFetcherVersion = 2;
  npmDepsHash = "sha256-F4q2JTPy1L33U6Hlh99Ehd2Cjwo9CET46JlKb2sGSWE=";

  nativeBuildInputs = [
    python311
    python3
    pkg-config
    nodejs
    gnumake
    gcc
    perl
  ];

  buildInputs = [
    libsecret
    vips
  ]
  ++ lib.optionals stdenv.hostPlatform.isLinux [
    libx11
  ];

  env = {
    NEXT_TELEMETRY_DISABLED = "1";
    # Turbopack's native memory use exceeds GitHub-hosted runner capacity on
    # current OmniRouter releases. Webpack is the upstream-supported fallback.
    OMNIROUTE_USE_TURBOPACK = "0";
    # v3.8.50 exceeds the former 6656 MiB V8 heap during the Webpack production
    # pass. Match the upstream native-build default so the clean build completes.
    OMNIROUTE_BUILD_MEMORY_MB = "8192";
    npm_config_arch = stdenv.hostPlatform.parsed.cpu.name;
    SHARP_IGNORE_GLOBAL_LIBVIPS = "0";
    ONNXRUNTIME_NODE_INSTALL = "skip";
    ONNXRUNTIME_NODE_INSTALL_CUDA = "skip";
  };

  npmRebuildFlags = [ "--ignore-scripts" ];

  doCheck = false;

  postPatch = ''
    if [ -f src/app/layout.tsx ]; then
      ${perl}/bin/perl -i -0pe '
        s|import \{ Inter \} from "next/font/google";\n?||g;
        s|const inter = Inter\(\{[^}]*\}\);|const inter = { variable: "--font-inter", className: "" };|gs;
      ' src/app/layout.tsx
    fi
  '';

  buildPhase = ''
    export NODE_ENV=production
    export CYPRESS_INSTALL_BINARY=0
    export HUSKY_SKIP_INSTALL=1
    npm rebuild better-sqlite3 --build-from-source
    ${lib.optionalString (stdenv.hostPlatform.system == "x86_64-linux") ''
      # SWC 1.16.13 rejects the overflow UID on Nix's sandbox root.
      tar -xOzf ${swcBinding} package/swc.linux-x64-gnu.node \
        > node_modules/@swc/core-linux-x64-gnu/swc.linux-x64-gnu.node
    ''}
    SWC_NATIVE_BINDING_CACHE=0 npm run build

    # Next.js preserves this npm bin link as an absolute build-time path.
    semverBin=.build/next/standalone/node_modules/global-agent/node_modules/.bin/semver
    if [ -L "$semverBin" ]; then
      ln -sfn ../semver/bin/semver.js "$semverBin"
    fi
  '';

  installPhase = ''
    runHook preInstall

    # The CLI imports TypeScript through the production tsx dependency.
    # Prune only dev dependencies after the standalone server is assembled.
    npm prune --omit=dev --ignore-scripts --offline

    mkdir -p $out/share/omnirouter
    test -f .build/next/standalone/server.js
    test -f .build/next/standalone/dev/run-standalone.mjs
    test -f bin/omniroute.mjs
    # Retain the production CLI tree, then copy only the assembled server.
    tar --exclude='./.build' --exclude='./.git' --exclude='./.env' \
      --exclude='./node_modules/.cache' -cf - . \
      | tar -C $out/share/omnirouter -xf -
    mkdir -p $out/share/omnirouter/.build/next
    cp -a .build/next/standalone $out/share/omnirouter/.build/next/

    mkdir -p $out/bin
    cat > $out/bin/omnirouter <<EOF
    #!/bin/sh
    cd $out/share/omnirouter/.build/next/standalone
    export DATA_DIR="\''${DATA_DIR:-\''${HOME:-/var/lib/omnirouter}}"
    export APP_LOG_FILE_PATH="\''${APP_LOG_FILE_PATH:-\''${DATA_DIR}/logs/application/app.log}"
    export NODE_ENV=production
    export OMNIROUTE_NO_UPDATE_NOTIFIER="\''${OMNIROUTE_NO_UPDATE_NOTIFIER:-1}"
    exec ${lib.getExe nodejs} dev/run-standalone.mjs "\$@"
    EOF

    chmod +x $out/bin/omnirouter

    cat > $out/bin/omniroute <<EOF
    #!/bin/sh
    cd $out/share/omnirouter
    export DATA_DIR="\''${DATA_DIR:-\''${HOME:-/var/lib/omnirouter}}"
    export NODE_ENV=production
    export OMNIROUTE_NO_UPDATE_NOTIFIER="\''${OMNIROUTE_NO_UPDATE_NOTIFIER:-1}"
    exec ${lib.getExe nodejs} bin/omniroute.mjs "\$@"
    EOF

    chmod +x $out/bin/omniroute

    runHook postInstall
  '';

  meta = with lib; {
    description = "OmniRouter: A versatile LLM router";
    homepage = "https://github.com/diegosouzapw/OmniRoute";
    license = licenses.mit;
  };
}
