import { Component, type ReactNode } from "react";
import { Button } from "@cloudflare/kumo";

interface ErrorBoundaryProps {
  children: ReactNode;
}

interface ErrorBoundaryState {
  hasError: boolean;
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = { hasError: false };
  }

  static getDerivedStateFromError(): ErrorBoundaryState {
    return { hasError: true };
  }

  handleReload = () => {
    window.location.reload();
  };

  render() {
    if (this.state.hasError) {
      return (
        <div className="flex min-h-screen flex-col items-center justify-center gap-4 bg-kumo-base p-8">
          <h2 className="text-lg font-semibold">页面发生错误</h2>
          <Button onClick={this.handleReload} variant="secondary">
            重新加载
          </Button>
        </div>
      );
    }
    return this.props.children;
  }
}
