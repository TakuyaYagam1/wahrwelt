{
  lib,
  wahrwelt,
  wahrweltLib,
  ...
}:

let
  desktopOrMore = wahrweltLib.presets.desktopOrMore wahrwelt;

  browser = [ "zen.desktop" ];
  editor = [ "code.desktop" ];
  fileManager = [ "thunar.desktop" ];
  imageViewer = [ "org.gnome.Loupe.desktop" ];
  videoPlayer = [ "mpv.desktop" ];
  pdfViewer = [ "org.pwmt.zathura.desktop" ];
  writer = [ "wps-office-wps.desktop" ];
  spreadsheet = [ "wps-office-et.desktop" ];
  presentation = [ "wps-office-wpp.desktop" ];

  codeDefaults = lib.genAttrs [
    "application/ecmascript"
    "application/javascript"
    "application/json"
    "application/sql"
    "application/toml"
    "application/typescript"
    "application/xml"
    "application/yaml"
    "application/x-httpd-php"
    "application/x-perl"
    "application/x-shellscript"
    "application/x-yaml"
    "text/css"
    "text/javascript"
    "text/markdown"
    "text/plain"
    "text/typescript"
    "text/yaml"
    "text/x-c"
    "text/x-chdr"
    "text/x-c++src"
    "text/x-c++hdr"
    "text/x-cmake"
    "text/x-csrc"
    "text/x-csharp"
    "text/x-dockerfile"
    "text/x-go"
    "text/x-haskell"
    "text/x-java"
    "text/x-javascript"
    "text/x-kotlin"
    "text/x-lua"
    "text/x-nix"
    "text/x-perl"
    "text/x-php"
    "text/x-python"
    "text/x-ruby"
    "text/x-rust"
    "text/x-scss"
    "text/x-shellscript"
    "text/x-terraform"
    "text/x-tex"
    "text/x-yaml"
    "text/xml"
  ] (_: editor);

  imageDefaults = lib.genAttrs [
    "image/avif"
    "image/bmp"
    "image/gif"
    "image/heic"
    "image/jpeg"
    "image/jpg"
    "image/jxl"
    "image/png"
    "image/svg+xml"
    "image/tiff"
    "image/vnd.microsoft.icon"
    "image/webp"
    "image/x-icon"
    "image/x-xcf"
  ] (_: imageViewer);

  videoDefaults = lib.genAttrs [
    "video/mp2t"
    "video/mp4"
    "video/mpeg"
    "video/ogg"
    "video/quicktime"
    "video/webm"
    "video/x-flv"
    "video/x-m4v"
    "video/x-matroska"
    "video/x-msvideo"
    "video/x-ms-wmv"
  ] (_: videoPlayer);

  audioDefaults = lib.genAttrs [
    "audio/aac"
    "audio/flac"
    "audio/mpeg"
    "audio/ogg"
    "audio/opus"
    "audio/wav"
    "audio/webm"
    "audio/x-matroska"
  ] (_: videoPlayer);

  writerDefaults = lib.genAttrs [
    "application/msword"
    "application/rtf"
    "application/vnd.ms-word"
    "application/vnd.ms-word.document.macroEnabled.12"
    "application/vnd.oasis.opendocument.text"
    "application/vnd.oasis.opendocument.text-flat-xml"
    "application/vnd.oasis.opendocument.text-template"
    "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
    "application/vnd.openxmlformats-officedocument.wordprocessingml.template"
    "application/x-doc"
    "application/x-fictionbook+xml"
    "application/x-mswrite"
    "text/rtf"
  ] (_: writer);

  spreadsheetDefaults = lib.genAttrs [
    "application/csv"
    "application/excel"
    "application/msexcel"
    "application/tab-separated-values"
    "application/vnd.ms-excel"
    "application/vnd.ms-excel.sheet.binary.macroEnabled.12"
    "application/vnd.ms-excel.sheet.macroEnabled.12"
    "application/vnd.ms-excel.template.macroEnabled.12"
    "application/vnd.oasis.opendocument.spreadsheet"
    "application/vnd.oasis.opendocument.spreadsheet-flat-xml"
    "application/vnd.oasis.opendocument.spreadsheet-template"
    "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
    "application/vnd.openxmlformats-officedocument.spreadsheetml.template"
    "text/comma-separated-values"
    "text/csv"
    "text/tab-separated-values"
    "text/x-csv"
  ] (_: spreadsheet);

  presentationDefaults = lib.genAttrs [
    "application/mspowerpoint"
    "application/vnd.ms-powerpoint"
    "application/vnd.ms-powerpoint.presentation.macroEnabled.12"
    "application/vnd.ms-powerpoint.slideshow.macroEnabled.12"
    "application/vnd.ms-powerpoint.template.macroEnabled.12"
    "application/vnd.oasis.opendocument.presentation"
    "application/vnd.oasis.opendocument.presentation-flat-xml"
    "application/vnd.oasis.opendocument.presentation-template"
    "application/vnd.openxmlformats-officedocument.presentationml.presentation"
    "application/vnd.openxmlformats-officedocument.presentationml.slideshow"
    "application/vnd.openxmlformats-officedocument.presentationml.template"
  ] (_: presentation);

  documentDefaults = {
    "application/pdf" = pdfViewer;
    "application/postscript" = pdfViewer;
    "image/vnd.djvu" = pdfViewer;

    "inode/directory" = fileManager;

    "application/x-extension-htm" = browser;
    "application/x-extension-html" = browser;
    "application/xhtml+xml" = browser;
    "text/html" = browser;
    "x-scheme-handler/about" = browser;
    "x-scheme-handler/ftp" = browser;
    "x-scheme-handler/http" = browser;
    "x-scheme-handler/https" = browser;
    "x-scheme-handler/unknown" = browser;
  };

  defaultApplications =
    imageDefaults
    // videoDefaults
    // audioDefaults
    // writerDefaults
    // spreadsheetDefaults
    // presentationDefaults
    // codeDefaults
    // documentDefaults;
in
{
  config = lib.mkIf desktopOrMore {
    xdg.mimeApps = {
      enable = true;
      associations.added = defaultApplications;
      inherit defaultApplications;
    };
  };
}
