import type { NextConfig } from "next";

const API_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
const SIGN_API_URL = process.env.NEXT_PUBLIC_SIGN_API_URL ?? "http://localhost:8084";

const nextConfig: NextConfig = {
  /* config options here */
  cacheComponents: true,
  partialPrefetching: true,
  turbopack: {
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
      },
    },
  },
  async rewrites() {
    return [
      { source: "/api/:path*", destination: `${API_URL}/api/:path*` },
      { source: "/esign/:path*", destination: `${SIGN_API_URL}/:path*` },
    ];
  },
};

export default nextConfig;
