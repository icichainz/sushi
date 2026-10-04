# Homebrew formula for the sushi command. Install with:
#   brew install icichainz/sushi/sushi
# (Homebrew finds this file through the repository itself, so no separate
# tap repository is needed.)
class Sushi < Formula
  desc "Terminal file manager for macOS"
  homepage "https://github.com/icichainz/sushi"
  url "https://github.com/icichainz/sushi/archive/refs/tags/v0.5.1.tar.gz"
  sha256 "d964caf9da254e95871f13f058215e528c860d982cac90a5965fdd44b440959b"
  license "MIT"
  head "https://github.com/icichainz/sushi.git", branch: "main"

  depends_on "go" => :build
  depends_on :macos

  def install
    system "go", "build", "-trimpath", "-ldflags", "-s -w -X main.version=#{version}", "-o", bin/"sushi", "."
  end

  test do
    assert_match "sushi #{version}", shell_output("#{bin}/sushi --version")
  end
end
