<script>
  let klass = '';
  export { klass as class };
</script>

<div class="hero-actions page-pad {klass}"><slot /></div>

<style>
  .hero-actions {
    display: flex;
    align-items: center;
    gap: 24px;
    min-height: 100px;
    padding-top: 20px;
    padding-bottom: 24px;
  }
  @media (max-width: 768px) {
    .hero-actions {
      gap: 16px;
    }
  }
</style>
